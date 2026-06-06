package ssh

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/k0sproject/rig/v2/protocol"
	"github.com/k0sproject/rig/v2/sshconfig"
	"github.com/k0sproject/rig/v2/sshconfig/options"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ssh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// withConfigParser temporarily installs a hermetic ssh config parser built from
// the given ssh_config content and restores the previous parser afterwards.
func withConfigParser(t *testing.T, content string) {
	t.Helper()
	parser, err := sshconfig.NewParser(strings.NewReader(content))
	require.NoError(t, err)
	prev := ConfigParser
	ConfigParser = parser
	t.Cleanup(func() { ConfigParser = prev })
}

// newTestConnection builds a Connection with explicit auth methods (to bypass
// key/agent loading) and an empty ConfigParser (to prevent ~/.ssh/config and
// /etc/ssh/ssh_config from affecting test behaviour). SSH_KNOWN_HOSTS is also
// cleared so that host-key validation does not depend on the developer's
// known_hosts file.
func newTestConnection(t *testing.T) *Connection {
	t.Helper()
	t.Setenv("SSH_KNOWN_HOSTS", "")
	t.Setenv("SSH_AUTH_SOCK", "")

	// Replace the global ConfigParser with one backed by empty readers so
	// the developer's ~/.ssh/config and /etc/ssh/ssh_config don't bleed into
	// these tests.
	oldParser := ConfigParser
	emptyParser, err := sshconfig.NewParser(strings.NewReader(""))
	require.NoError(t, err, "sshconfig.NewParser must succeed for isolated tests")
	ConfigParser = emptyParser
	t.Cleanup(func() { ConfigParser = oldParser })

	c, err := NewConnection(Config{
		Address:     "127.0.0.1",
		User:        "test",
		Port:        22,
		AuthMethods: []ssh.AuthMethod{ssh.Password("test")},
	})
	require.NoError(t, err)
	require.NotNil(t, c.sshConfig)
	return c
}

// writeEncryptedKey generates an ed25519 private key encrypted with a
// passphrase and writes it to a temp file, returning its path. Parsing such a
// key without the passphrase yields ssh.PassphraseMissingError, which is the
// branch pkeySigner consults BatchMode in.
func writeEncryptedKey(t *testing.T) string {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	block, err := ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte("secret"))
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "id_ed25519")
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(block), 0o600))
	return path
}

func TestPkeySignerBatchModeSkipsEncryptedKey(t *testing.T) {
	ctx := context.Background()
	c := newTestConnection(t)
	c.sshConfig.BatchMode = options.BooleanOption("yes")

	path := writeEncryptedKey(t)

	_, _, err := c.pkeySigner(ctx, nil, path)
	require.Error(t, err)
	require.ErrorIs(t, err, protocol.ErrNonRetryable)
	require.NotContains(t, err.Error(), "can't parse keyfile",
		"BatchMode should short-circuit before the generic parse-failure path")
	require.NotContains(t, err.Error(), "skip signer cache",
		"sentinel text must not appear in user-facing error messages")
}

func TestPkeySignerEncryptedKeyWithoutBatchModeOrCallback(t *testing.T) {
	ctx := context.Background()
	c := newTestConnection(t)
	// BatchMode unset and no PasswordCallback: pkeySigner returns a non-retryable,
	// non-cacheable "no password callback" error — not the generic parse-failure path.
	c.PasswordCallback = nil

	path := writeEncryptedKey(t)

	_, _, err := c.pkeySigner(ctx, nil, path)
	require.Error(t, err)
	require.ErrorIs(t, err, protocol.ErrNonRetryable)
	require.ErrorIs(t, err, errSkipCache, "no-callback error must carry errSkipCache so it is not permanently cached")
	require.Contains(t, err.Error(), "no password callback")
	require.NotContains(t, err.Error(), "skip signer cache",
		"sentinel text must not appear in user-facing error messages")
}

// TestPkeySignerBatchModeErrorNonCacheable guards against signer-cache poisoning:
// a BatchMode=yes connection must not permanently cache its "skip" error so that
// a later non-batch connection to the same key path still gets a chance to decrypt.
func TestPkeySignerBatchModeErrorNonCacheable(t *testing.T) {
	ctx := context.Background()
	c := newTestConnection(t)
	c.sshConfig.BatchMode = options.BooleanOption("yes")
	path := writeEncryptedKey(t)

	_, _, err := c.pkeySigner(ctx, nil, path)
	require.ErrorIs(t, err, errSkipCache, "batch-mode skip error must carry errSkipCache so clientConfig does not cache it")
}

// TestLoadKeySignersAgentBackedNotCached verifies that signers obtained from
// the SSH agent (fromAgent=true) are not stored in signerCache, preventing
// stale references after the agent connection is closed.
func TestLoadKeySignersAgentBackedNotCached(t *testing.T) {
	ctx := context.Background()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	sshPub, err := ssh.NewPublicKey(pub)
	require.NoError(t, err)

	sshSigner, err := ssh.NewSignerFromKey(priv)
	require.NoError(t, err)

	pubKeyFile := filepath.Join(t.TempDir(), "id_ed25519.pub")
	require.NoError(t, os.WriteFile(pubKeyFile, ssh.MarshalAuthorizedKey(sshPub), 0o600))

	signerCache.Delete(pubKeyFile)
	t.Cleanup(func() { signerCache.Delete(pubKeyFile) })

	c := newTestConnection(t)
	c.keyPaths = []string{pubKeyFile}

	signers := c.loadKeySigners(ctx, []ssh.Signer{sshSigner})
	require.Len(t, signers, 1, "agent-backed signer must be returned")

	_, cached := signerCache.Load(pubKeyFile)
	require.False(t, cached, "agent-backed signer must not be stored in signerCache")
}

// TestClientConfigPubkeyAuthenticationDisabled verifies that when the ssh config
// has PubkeyAuthentication set to "no", clientConfig skips all public key
// authentication (ssh agent and identity files). With no AuthMethods provided,
// this leaves no usable authentication method and must return a non-retryable
// error.
func TestClientConfigPubkeyAuthenticationDisabled(t *testing.T) {
	// Empty SSH_KNOWN_HOSTS makes hostkeyCallback return an insecure-ignore
	// callback, so the test does not depend on any known_hosts file on disk.
	t.Setenv("SSH_KNOWN_HOSTS", "")

	c := &Connection{
		Config: Config{
			Address: "127.0.0.1",
			User:    "test",
			Port:    22,
		},
		sshConfig: &sshconfig.Config{
			PubkeyAuthentication: options.PubkeyAuthenticationOptionNo,
		},
		keyPaths: []string{"/some/fake/path"},
	}

	cfg, agentClose, err := c.clientConfig(context.Background())
	agentClose()
	require.Error(t, err)
	require.Nil(t, cfg)
	require.ErrorIs(t, err, protocol.ErrNonRetryable)
	require.Contains(t, err.Error(), "no usable authentication method")
}

func TestNewConnectionServerAliveIntervalWiresKeepalive(t *testing.T) {
	withConfigParser(t, "Host *\n  ServerAliveInterval 60\n")

	conn, err := NewConnection(Config{Address: "host.example.com", Port: 22, User: "user"})
	require.NoError(t, err)
	require.NotNil(t, conn.options.KeepAliveInterval)
	assert.Equal(t, 60*time.Second, *conn.options.KeepAliveInterval)
}

func TestNewConnectionExplicitKeepaliveOverridesServerAliveInterval(t *testing.T) {
	withConfigParser(t, "Host *\n  ServerAliveInterval 60\n")

	conn, err := NewConnection(Config{Address: "host.example.com", Port: 22, User: "user"}, WithKeepAlive(10*time.Second))
	require.NoError(t, err)
	require.NotNil(t, conn.options.KeepAliveInterval)
	assert.Equal(t, 10*time.Second, *conn.options.KeepAliveInterval)
}

func TestNewConnectionNoServerAliveIntervalLeavesKeepaliveUnset(t *testing.T) {
	// With no parser the resolved sshConfig.ServerAliveInterval stays zero, so the
	// guard must leave KeepAliveInterval unset. Using a real parser is not hermetic
	// here because some platforms ship a non-zero ServerAliveInterval default
	// (e.g. macOS defaults to 30).
	prev := ConfigParser
	ConfigParser = nil
	t.Cleanup(func() { ConfigParser = prev })

	conn, err := NewConnection(Config{Address: "host.example.com", Port: 22, User: "user"})
	require.NoError(t, err)
	assert.Nil(t, conn.options.KeepAliveInterval)
}

func TestWithKeepAliveZeroDisablesKeepalive(t *testing.T) {
	withConfigParser(t, "Host *\n  ServerAliveInterval 60\n")

	conn, err := NewConnection(Config{Address: "host.example.com", Port: 22, User: "user"}, WithKeepAlive(0))
	require.NoError(t, err)
	// startKeepalive must treat <= 0 as disabled; verify it does not panic.
	conn.mu.Lock()
	conn.startKeepalive()
	conn.mu.Unlock()
	assert.Nil(t, conn.done, "zero interval must not start keepalive goroutine")
}

func TestClientConfigAlgorithmFields(t *testing.T) {
	ctx := context.Background()
	c := newTestConnection(t)

	c.sshConfig.Ciphers = []string{"aes128-ctr", "aes256-ctr"}
	c.sshConfig.KexAlgorithms = []string{"curve25519-sha256"}
	c.sshConfig.MACs = []string{"hmac-sha2-256"}
	c.sshConfig.HostKeyAlgorithms = []string{"ssh-ed25519"}

	config, agentClose, err := c.clientConfig(ctx)
	defer agentClose()
	require.NoError(t, err)
	require.NotNil(t, config)

	require.Equal(t, []string{"aes128-ctr", "aes256-ctr"}, config.Ciphers)
	require.Equal(t, []string{"curve25519-sha256"}, config.KeyExchanges)
	require.Equal(t, []string{"hmac-sha2-256"}, config.MACs)
	require.Equal(t, []string{"ssh-ed25519"}, config.HostKeyAlgorithms)
}

func TestClientConfigAlgorithmFieldsEmpty(t *testing.T) {
	ctx := context.Background()
	c := newTestConnection(t)

	// Explicitly clear the parser-resolved defaults so the test is hermetic and
	// does not depend on the machine's ssh_config. With nil sshconfig fields,
	// clientConfig must leave the ssh.ClientConfig fields nil so crypto/ssh's
	// built-in defaults apply.
	c.sshConfig.Ciphers = nil
	c.sshConfig.KexAlgorithms = nil
	c.sshConfig.MACs = nil
	c.sshConfig.HostKeyAlgorithms = nil

	config, agentClose, err := c.clientConfig(ctx)
	defer agentClose()
	require.NoError(t, err)
	require.NotNil(t, config)

	require.Nil(t, config.Ciphers)
	require.Nil(t, config.KeyExchanges)
	require.Nil(t, config.MACs)
	require.Nil(t, config.HostKeyAlgorithms)
}

// TestClientConfigIdentitiesOnly is a smoke test that verifies setting
// IdentitiesOnly does not break config construction when AuthMethods are
// provided. When AuthMethods are set, clientConfig still loads SSH-agent
// signers (needed for key decryption) but skips identity-file and agent
// auth method assembly. The IdentitiesOnly agent-skip path is not exercised
// here because it only applies to the auth method assembly step.
func TestClientConfigIdentitiesOnly(t *testing.T) {
	ctx := context.Background()
	c := newTestConnection(t)

	c.sshConfig.IdentitiesOnly = options.BooleanOption("yes")
	require.True(t, c.sshConfig.IdentitiesOnly.IsTrue())

	config, agentClose, err := c.clientConfig(ctx)
	defer agentClose()
	require.NoError(t, err)
	require.NotNil(t, config)
	require.Len(t, config.Auth, 1)
}

// TestClientConfigIdentitiesOnlyAgentFiltering verifies that IdentitiesOnly=yes
// prevents offering unrelated agent keys as auth methods while still allowing
// the agent to provide signers for explicitly configured IdentityFile public keys.
func TestClientConfigIdentitiesOnlyAgentFiltering(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-socket ssh-agent not available on windows")
	}

	ctx := context.Background()

	// Key A: has its private key in the agent and a .pub IdentityFile.
	// Key B: unrelated key held only in the agent (no IdentityFile).
	_, privA, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	_, privB, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	signerA, err := ssh.NewSignerFromKey(privA)
	require.NoError(t, err)

	keyring := agent.NewKeyring()
	require.NoError(t, keyring.Add(agent.AddedKey{PrivateKey: privA}))
	require.NoError(t, keyring.Add(agent.AddedKey{PrivateKey: privB}))

	// On darwin, os.TempDir() returns a long $TMPDIR path that exceeds the
	// 104-byte unix socket path limit; use /tmp which is always short there.
	baseDir := ""
	if runtime.GOOS == "darwin" {
		baseDir = "/tmp"
	}
	dir, err := os.MkdirTemp(baseDir, "rig")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	socketPath := filepath.Join(dir, "agent.sock")
	ln, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_ = agent.ServeAgent(keyring, conn)
				conn.Close()
			}()
		}
	}()

	pubKeyPath := filepath.Join(dir, "id_ed25519.pub")
	require.NoError(t, os.WriteFile(pubKeyPath, ssh.MarshalAuthorizedKey(signerA.PublicKey()), 0o600))

	t.Setenv("SSH_AUTH_SOCK", socketPath)
	t.Setenv("SSH_KNOWN_HOSTS", "")

	// Null out ConfigParser so NewConnection does not read ~/.ssh/config or
	// /etc/ssh/ssh_config, keeping each sub-test hermetic.
	savedParser := ConfigParser
	ConfigParser = nil
	t.Cleanup(func() { ConfigParser = savedParser })

	newConn := func(identityFile string, identitiesOnly bool) *Connection {
		t.Helper()
		c, cerr := NewConnection(Config{Address: "127.0.0.1", User: "test", Port: 22})
		require.NoError(t, cerr)
		// Override any ssh_config-resolved identity files for test isolation.
		if identityFile != "" {
			c.sshConfig.IdentityFile = []string{identityFile}
		} else {
			c.sshConfig.IdentityFile = nil
		}
		if identitiesOnly {
			c.sshConfig.IdentitiesOnly = options.BooleanOption("yes")
		}
		c.SetDefaults(ctx)
		return c
	}

	t.Run("IdentitiesOnly=true suppresses wholesale agent keys", func(t *testing.T) {
		// No IdentityFile, IdentitiesOnly=true: agent keys are not offered → no usable auth.
		c := newConn("", true)
		_, agentClose, err := c.clientConfig(ctx)
		agentClose()
		require.Error(t, err)
		require.ErrorIs(t, err, protocol.ErrNonRetryable)
	})

	t.Run("IdentitiesOnly=false offers all agent keys", func(t *testing.T) {
		// No IdentityFile, IdentitiesOnly=false: agent keys are offered.
		c := newConn("", false)
		config, agentClose, err := c.clientConfig(ctx)
		defer agentClose()
		require.NoError(t, err)
		require.Len(t, config.Auth, 1)
	})

	t.Run("IdentitiesOnly=true still resolves agent-backed IdentityFile pub key", func(t *testing.T) {
		// IdentityFile points to key A's .pub; private key is in agent.
		// Even with IdentitiesOnly=true, pkeySigner should find key A via the agent.
		c := newConn(pubKeyPath, true)
		config, agentClose, err := c.clientConfig(ctx)
		defer agentClose()
		require.NoError(t, err)
		require.Len(t, config.Auth, 1)
	})
}

func TestDialNetwork(t *testing.T) {
	cases := []struct {
		addressFamily string
		want          string
	}{
		{"any", "tcp"},
		{"inet", "tcp4"},
		{"inet6", "tcp6"},
		{"", "tcp"},
	}
	for _, tc := range cases {
		c := &Connection{sshConfig: &sshconfig.Config{}}
		c.sshConfig.AddressFamily = tc.addressFamily
		require.Equal(t, tc.want, c.dialNetwork(), "AddressFamily=%q", tc.addressFamily)
	}
}

// newBlockingSSHClient creates an in-process SSH connection whose Dial always
// blocks until the connection is closed. The server completes the SSH
// handshake but never responds to channel-open requests, so any call to
// client.Dial blocks waiting for a channel-open confirmation that never
// arrives. t.Cleanup closes the client.
func newBlockingSSHClient(t *testing.T) *ssh.Client {
	t.Helper()

	_, hostKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(hostKey)
	require.NoError(t, err)

	serverCfg := &ssh.ServerConfig{NoClientAuth: true}
	serverCfg.AddHostKey(signer)

	// Use a real TCP listener so both sides can write their SSH version strings
	// concurrently without deadlocking (net.Pipe is synchronous; two
	// simultaneous writes deadlock because neither side is reading yet).
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })

	serverConnCh := make(chan net.Conn, 1)
	go func() {
		serverEnd, err := ln.Accept()
		if err != nil {
			return
		}
		serverConnCh <- serverEnd
	}()

	clientEnd, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)

	serverEnd := <-serverConnCh

	go func() {
		defer serverEnd.Close()
		sConn, chans, reqs, err := ssh.NewServerConn(serverEnd, serverCfg)
		if err != nil {
			return
		}
		go ssh.DiscardRequests(reqs)
		go func() {
			for range chans {
				// drain without responding; callers of client.Dial block
				// until the connection is closed
			}
		}()
		_ = sConn.Wait()
	}()

	clientConn, clientChans, clientReqs, err := ssh.NewClientConn(clientEnd, "test", &ssh.ClientConfig{
		User:            "test",
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	})
	require.NoError(t, err)

	client := ssh.NewClient(clientConn, clientChans, clientReqs)
	t.Cleanup(func() { client.Close() })
	return client
}

// TestDialWithDeadlineContextCancelled verifies that dialWithDeadline aborts
// and tears down the bastion connection when the context is already cancelled
// on entry.
func TestDialWithDeadlineContextCancelled(t *testing.T) {
	c := &Connection{sshConfig: &sshconfig.Config{}, options: NewOptions()}
	c.client = newBlockingSSHClient(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.dialWithDeadline(ctx, time.Time{}, "127.0.0.1:2222")
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)

	c.mu.Lock()
	got := c.client
	c.mu.Unlock()
	require.Nil(t, got, "Disconnect must clear c.client when context is cancelled")
}

// TestDialWithDeadlineDeadlineExpired verifies that dialWithDeadline aborts
// when the supplied deadline fires before the dial completes.
func TestDialWithDeadlineDeadlineExpired(t *testing.T) {
	c := &Connection{sshConfig: &sshconfig.Config{}, options: NewOptions()}
	c.client = newBlockingSSHClient(t)

	deadline := time.Now().Add(50 * time.Millisecond)
	_, err := c.dialWithDeadline(context.Background(), deadline, "127.0.0.1:2222")
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	c.mu.Lock()
	got := c.client
	c.mu.Unlock()
	require.Nil(t, got, "Disconnect must clear c.client when deadline expires")
}

func TestConnectDeadline(t *testing.T) {
	t.Run("no timeout no context deadline returns zero", func(t *testing.T) {
		c := &Connection{sshConfig: &sshconfig.Config{}}
		require.True(t, c.connectDeadline(context.Background()).IsZero())
	})

	t.Run("ConnectTimeout takes effect", func(t *testing.T) {
		c := &Connection{sshConfig: &sshconfig.Config{}}
		c.sshConfig.ConnectTimeout = 5 * time.Second
		before := time.Now()
		d := c.connectDeadline(context.Background())
		require.False(t, d.IsZero())
		require.True(t, d.After(before))
		require.True(t, d.Before(before.Add(6*time.Second)))
	})

	t.Run("context deadline earlier than ConnectTimeout wins", func(t *testing.T) {
		c := &Connection{sshConfig: &sshconfig.Config{}}
		c.sshConfig.ConnectTimeout = 60 * time.Second
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		ctxDeadline, _ := ctx.Deadline()
		d := c.connectDeadline(ctx)
		require.Equal(t, ctxDeadline, d)
	})

	t.Run("ConnectTimeout earlier than context deadline wins", func(t *testing.T) {
		c := &Connection{sshConfig: &sshconfig.Config{}}
		c.sshConfig.ConnectTimeout = 1 * time.Second
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		before := time.Now()
		d := c.connectDeadline(ctx)
		require.True(t, d.After(before))
		require.True(t, d.Before(before.Add(2*time.Second)))
	})
}

func TestNewConnectionSSHConfigOptions(t *testing.T) {
	t.Setenv("SSH_KNOWN_HOSTS", "")

	prev := ConfigParser
	ConfigParser = nil
	t.Cleanup(func() { ConfigParser = prev })

	t.Run("unknown option returns ErrValidationFailed", func(t *testing.T) {
		_, err := NewConnection(Config{
			Address:          "host.example.com",
			Port:             22,
			User:             "user",
			SSHConfigOptions: sshconfig.OptionArguments{"NoSuchOption": "value"},
		})
		require.Error(t, err)
		require.ErrorIs(t, err, protocol.ErrValidationFailed)
	})

	t.Run("valid option is applied before ConfigParser", func(t *testing.T) {
		withConfigParser(t, "Host *\n  Compression no\n")
		conn, err := NewConnection(Config{
			Address:          "host.example.com",
			Port:             22,
			User:             "user",
			SSHConfigOptions: sshconfig.OptionArguments{"Compression": true},
		})
		require.NoError(t, err)
		require.True(t, conn.sshConfig.Compression.IsTrue(),
			"SSHConfigOptions must take precedence over ConfigParser")
	})
}

// TestLoadAgentSignersIdentityAgent verifies that the IdentityAgent ssh config
// field controls which agent socket is used (or skipped) for agent-backed signers.
func TestLoadAgentSignersIdentityAgent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-socket ssh-agent not available on windows")
	}

	ctx := context.Background()

	startAgent := func(t *testing.T, socketPath string) ssh.Signer {
		t.Helper()
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)
		signer, err := ssh.NewSignerFromKey(priv)
		require.NoError(t, err)
		keyring := agent.NewKeyring()
		require.NoError(t, keyring.Add(agent.AddedKey{PrivateKey: priv}))
		ln, err := net.Listen("unix", socketPath)
		require.NoError(t, err)
		t.Cleanup(func() { _ = ln.Close() })
		go func() {
			for {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				go func() {
					_ = agent.ServeAgent(keyring, conn)
					conn.Close()
				}()
			}
		}()
		return signer
	}

	// Use /tmp directly to keep unix socket paths short (macOS limit: 104 chars).
	dir, err := os.MkdirTemp("/tmp", "rig")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sockA := filepath.Join(dir, "a.sock")
	sockB := filepath.Join(dir, "b.sock")
	startAgent(t, sockA)
	signerB := startAgent(t, sockB)

	savedParser := ConfigParser
	ConfigParser = nil
	t.Cleanup(func() { ConfigParser = savedParser })

	newConn := func(identityAgent options.IdentityAgentOption) *Connection {
		t.Helper()
		c, cerr := NewConnection(Config{Address: "127.0.0.1", User: "test", Port: 22})
		require.NoError(t, cerr)
		c.sshConfig.IdentityAgent = identityAgent
		c.sshConfig.IdentityFile = nil
		c.SetDefaults(ctx)
		return c
	}

	t.Run("IdentityAgent=none skips agent even when SSH_AUTH_SOCK is set", func(t *testing.T) {
		t.Setenv("SSH_AUTH_SOCK", sockA)
		c := newConn(options.IdentityAgentOption("none"))
		signers, closeAgent := c.loadAgentSigners(ctx)
		closeAgent()
		require.Empty(t, signers, "IdentityAgent=none must yield no signers")
	})

	t.Run("IdentityAgent=custom socket uses that socket not SSH_AUTH_SOCK", func(t *testing.T) {
		t.Setenv("SSH_AUTH_SOCK", sockA)
		c := newConn(options.IdentityAgentOption(sockB))
		signers, closeAgent := c.loadAgentSigners(ctx)
		defer closeAgent()
		require.Len(t, signers, 1, "must get exactly one signer from the custom agent socket")
		require.Equal(t, signerB.PublicKey().Marshal(), signers[0].PublicKey().Marshal(), "signer must come from sockB not SSH_AUTH_SOCK")
	})
}

func TestClientConfigRekeyLimit(t *testing.T) {
	t.Setenv("SSH_KNOWN_HOSTS", "")

	orig := ConfigParser
	ConfigParser = nil
	t.Cleanup(func() { ConfigParser = orig })

	conn, err := NewConnection(Config{Address: "127.0.0.1"})
	require.NoError(t, err)

	conn.sshConfig.RekeyLimit = options.RekeyLimitOption{MaxData: 1024 * 1024}
	conn.Config.AuthMethods = []ssh.AuthMethod{ssh.Password("dummy")}

	cfg, agentClose, err := conn.clientConfig(context.Background())
	agentClose()
	require.NoError(t, err)
	require.Equal(t, uint64(1024*1024), cfg.RekeyThreshold)
}

// unsetKnownHostsEnv ensures SSH_KNOWN_HOSTS is not set for the duration of the
// test so the UserKnownHostsFile/GlobalKnownHostsFile resolution path is
// exercised instead of the environment override.
func unsetKnownHostsEnv(t *testing.T) {
	t.Helper()
	prev, ok := os.LookupEnv("SSH_KNOWN_HOSTS")
	require.NoError(t, os.Unsetenv("SSH_KNOWN_HOSTS"))
	t.Cleanup(func() {
		if ok {
			require.NoError(t, os.Setenv("SSH_KNOWN_HOSTS", prev))
		} else {
			require.NoError(t, os.Unsetenv("SSH_KNOWN_HOSTS"))
		}
	})
}

func TestHostkeyCallbackUserNoneFallsBackToGlobalKnownHostsFile(t *testing.T) {
	unsetKnownHostsEnv(t)

	khPath := filepath.Join(t.TempDir(), "ssh_known_hosts")
	require.NoError(t, os.WriteFile(khPath, []byte(""), 0o600))

	c := &Connection{
		sshConfig: &sshconfig.Config{
			// "none" is the OpenSSH sentinel meaning "skip user known_hosts".
			UserKnownHostsFile:   []string{"none"},
			GlobalKnownHostsFile: []string{khPath},
		},
	}

	cb, err := c.hostkeyCallback(context.Background())
	require.NoError(t, err)
	require.NotNil(t, cb)
}

func TestHostkeyCallbackUserNoneAfterValidPathFallsBackToGlobal(t *testing.T) {
	unsetKnownHostsEnv(t)

	userKH := filepath.Join(t.TempDir(), "user_known_hosts")
	require.NoError(t, os.WriteFile(userKH, []byte(""), 0o600))
	globalKH := filepath.Join(t.TempDir(), "ssh_known_hosts")
	require.NoError(t, os.WriteFile(globalKH, []byte(""), 0o600))

	c := &Connection{
		sshConfig: &sshconfig.Config{
			// "none" after a valid path must still disable user known_hosts.
			UserKnownHostsFile:   []string{userKH, "none"},
			GlobalKnownHostsFile: []string{globalKH},
		},
	}

	cb, err := c.hostkeyCallback(context.Background())
	require.NoError(t, err)
	require.NotNil(t, cb)
}

func TestHostkeyCallbackFallsBackToGlobalKnownHostsFile(t *testing.T) {
	unsetKnownHostsEnv(t)

	khPath := filepath.Join(t.TempDir(), "ssh_known_hosts")
	require.NoError(t, os.WriteFile(khPath, []byte(""), 0o600))

	c := &Connection{
		sshConfig: &sshconfig.Config{
			// No user known_hosts file: resolution must fall through.
			UserKnownHostsFile:   []string{},
			GlobalKnownHostsFile: []string{khPath},
		},
	}

	cb, err := c.hostkeyCallback(context.Background())
	require.NoError(t, err)
	require.NotNil(t, cb)
}

func TestHostkeyCallbackNoKnownHostsFile(t *testing.T) {
	unsetKnownHostsEnv(t)

	c := &Connection{
		sshConfig: &sshconfig.Config{
			UserKnownHostsFile:   []string{},
			GlobalKnownHostsFile: []string{},
		},
	}

	_, err := c.hostkeyCallback(context.Background())
	require.Error(t, err)
}

func TestHostkeyCallbackSkipsMissingGlobalKnownHostsFile(t *testing.T) {
	unsetKnownHostsEnv(t)

	missing := filepath.Join(t.TempDir(), "nonexistent_known_hosts")

	c := &Connection{
		sshConfig: &sshconfig.Config{
			UserKnownHostsFile:   []string{},
			GlobalKnownHostsFile: []string{missing},
		},
	}

	_, err := c.hostkeyCallback(context.Background())
	require.Error(t, err, "missing global known_hosts must not be created — should fall through to error")

	_, statErr := os.Stat(missing)
	require.True(t, os.IsNotExist(statErr), "hostkeyCallback must not create missing global known_hosts files")
}

func TestSelectBindAddr(t *testing.T) {
	loopback4 := &net.IPNet{IP: net.ParseIP("127.0.0.1"), Mask: net.CIDRMask(8, 32)}
	linkLocal4 := &net.IPNet{IP: net.ParseIP("169.254.1.1"), Mask: net.CIDRMask(16, 32)}
	global4 := &net.IPNet{IP: net.ParseIP("192.168.1.10"), Mask: net.CIDRMask(24, 32)}
	global6 := &net.IPNet{IP: net.ParseIP("2001:db8::1"), Mask: net.CIDRMask(32, 128)}

	cases := []struct {
		name   string
		addrs  []net.Addr
		family string
		wantIP net.IP
	}{
		{"empty list returns nil", nil, "tcp", nil},
		{"loopback excluded", []net.Addr{loopback4}, "tcp", nil},
		{"link-local excluded", []net.Addr{linkLocal4}, "tcp", nil},
		{"global IPv4 selected for tcp", []net.Addr{global4}, "tcp", global4.IP},
		{"global IPv4 selected for tcp4", []net.Addr{global4}, "tcp4", global4.IP},
		{"global IPv4 skipped for tcp6", []net.Addr{global4}, "tcp6", nil},
		{"global IPv6 selected for tcp", []net.Addr{global6}, "tcp", global6.IP},
		{"global IPv6 selected for tcp6", []net.Addr{global6}, "tcp6", global6.IP},
		{"global IPv6 skipped for tcp4", []net.Addr{global6}, "tcp4", nil},
		{"prefers first global in mixed list", []net.Addr{loopback4, global4, global6}, "tcp", global4.IP},
		{"tcp4 skips leading IPv6 and picks IPv4", []net.Addr{global6, global4}, "tcp4", global4.IP},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := selectBindAddr(tc.addrs, tc.family)
			if tc.wantIP == nil {
				require.Nil(t, got)
			} else {
				require.NotNil(t, got)
				require.True(t, tc.wantIP.Equal(got), "got %v, want %v", got, tc.wantIP)
			}
		})
	}
}

func TestLocalAddrBindAddress(t *testing.T) {
	ctx := context.Background()

	t.Run("valid IPv4 returns TCPAddr", func(t *testing.T) {
		c := &Connection{sshConfig: &sshconfig.Config{BindAddress: "10.0.0.1"}}
		addr := c.localAddr(ctx)
		require.NotNil(t, addr)
		tcp, ok := addr.(*net.TCPAddr)
		require.True(t, ok)
		require.True(t, net.ParseIP("10.0.0.1").Equal(tcp.IP))
	})

	t.Run("valid IPv6 returns TCPAddr", func(t *testing.T) {
		c := &Connection{sshConfig: &sshconfig.Config{BindAddress: "::1"}}
		addr := c.localAddr(ctx)
		require.NotNil(t, addr)
		tcp, ok := addr.(*net.TCPAddr)
		require.True(t, ok)
		require.True(t, net.ParseIP("::1").Equal(tcp.IP))
	})

	t.Run("invalid IP returns nil", func(t *testing.T) {
		c := &Connection{sshConfig: &sshconfig.Config{BindAddress: "not-an-ip"}}
		require.Nil(t, c.localAddr(ctx))
	})

	t.Run("IPv6 BindAddress with AddressFamily inet returns nil", func(t *testing.T) {
		c := &Connection{sshConfig: &sshconfig.Config{BindAddress: "2001:db8::1", AddressFamily: "inet"}}
		require.Nil(t, c.localAddr(ctx))
	})

	t.Run("IPv4 BindAddress with AddressFamily inet6 returns nil", func(t *testing.T) {
		c := &Connection{sshConfig: &sshconfig.Config{BindAddress: "10.0.0.1", AddressFamily: "inet6"}}
		require.Nil(t, c.localAddr(ctx))
	})

	t.Run("IPv4 BindAddress with AddressFamily inet returns TCPAddr", func(t *testing.T) {
		c := &Connection{sshConfig: &sshconfig.Config{BindAddress: "10.0.0.1", AddressFamily: "inet"}}
		addr := c.localAddr(ctx)
		require.NotNil(t, addr)
		tcp, ok := addr.(*net.TCPAddr)
		require.True(t, ok)
		require.True(t, net.ParseIP("10.0.0.1").Equal(tcp.IP))
	})

	t.Run("both fields unset returns nil", func(t *testing.T) {
		c := &Connection{sshConfig: &sshconfig.Config{}}
		require.Nil(t, c.localAddr(ctx))
	})
}

func TestLocalAddrBindInterface(t *testing.T) {
	ctx := context.Background()

	t.Run("nonexistent interface returns nil", func(t *testing.T) {
		c := &Connection{sshConfig: &sshconfig.Config{BindInterface: "rig-no-such-iface"}}
		require.Nil(t, c.localAddr(ctx))
	})

	t.Run("invalid BindAddress falls through to BindInterface", func(t *testing.T) {
		// BindAddress is unusable; BindInterface is probed next (nonexistent → nil).
		c := &Connection{sshConfig: &sshconfig.Config{BindAddress: "not-an-ip", BindInterface: "rig-no-such-iface"}}
		require.Nil(t, c.localAddr(ctx))
	})

	t.Run("mismatched BindAddress falls through to BindInterface", func(t *testing.T) {
		// BindAddress family conflicts with AddressFamily; BindInterface is probed next.
		c := &Connection{sshConfig: &sshconfig.Config{BindAddress: "2001:db8::1", AddressFamily: "inet", BindInterface: "rig-no-such-iface"}}
		require.Nil(t, c.localAddr(ctx))
	})
}

// makeCertForSigner generates a CA-signed SSH user certificate for the given signer.
func makeCertForSigner(t *testing.T, signer ssh.Signer) *ssh.Certificate {
	t.Helper()
	_, caPriv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	caSigner, err := ssh.NewSignerFromKey(caPriv)
	require.NoError(t, err)
	cert := &ssh.Certificate{
		Key:         signer.PublicKey(),
		CertType:    ssh.UserCert,
		KeyId:       "test",
		ValidAfter:  0,
		ValidBefore: ssh.CertTimeInfinity,
	}
	require.NoError(t, cert.SignCert(rand.Reader, caSigner))
	return cert
}

// writeCert marshals cert to authorized_keys format and writes it to path.
func writeCert(t *testing.T, path string, cert *ssh.Certificate) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, ssh.MarshalAuthorizedKey(cert), 0o600))
}

// TestCertSignerForSignerImplicit verifies that the implicit <keypath>-cert.pub
// path is loaded and produces a cert signer when the cert matches the key.
func TestCertSignerForSignerImplicit(t *testing.T) {
	ctx := context.Background()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(priv)
	require.NoError(t, err)

	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519")
	cert := makeCertForSigner(t, signer)
	writeCert(t, keyPath+"-cert.pub", cert)

	c := &Connection{sshConfig: &sshconfig.Config{}}
	cs := c.certSignerForSigner(ctx, signer, keyPath)

	require.NotNil(t, cs, "implicit cert path must produce a cert signer")
	_, ok := cs.PublicKey().(*ssh.Certificate)
	require.True(t, ok, "cert signer must present a certificate as its public key")
}

// TestCertSignerForSignerExplicit verifies that explicit CertificateFile entries
// are tried when the implicit path is absent.
func TestCertSignerForSignerExplicit(t *testing.T) {
	ctx := context.Background()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(priv)
	require.NoError(t, err)

	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519")
	// no implicit cert
	certPath := filepath.Join(dir, "explicit-cert.pub")
	cert := makeCertForSigner(t, signer)
	writeCert(t, certPath, cert)

	c := &Connection{sshConfig: &sshconfig.Config{CertificateFile: []string{certPath}}}
	cs := c.certSignerForSigner(ctx, signer, keyPath)

	require.NotNil(t, cs, "explicit CertificateFile must produce a cert signer")
	_, ok := cs.PublicKey().(*ssh.Certificate)
	require.True(t, ok, "cert signer must present a certificate as its public key")
}

// TestCertSignerForSignerExplicitUnexpanded verifies that a CertificateFile entry
// with a tilde prefix is expanded before use, covering configs constructed without
// the sshconfig parser's Finalize step.
func TestCertSignerForSignerExplicitUnexpanded(t *testing.T) {
	ctx := context.Background()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(priv)
	require.NoError(t, err)

	// Redirect HOME to a temp dir so ~ expansion is hermetic and never touches
	// the real home directory.
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	t.Setenv("USERPROFILE", fakeHome)

	certPath := filepath.Join(fakeHome, "id_ed25519-cert.pub")
	cert := makeCertForSigner(t, signer)
	writeCert(t, certPath, cert)

	// Pass the unexpanded tilde path as CertificateFile would be before Finalize.
	c := &Connection{sshConfig: &sshconfig.Config{CertificateFile: []string{"~/id_ed25519-cert.pub"}}}
	cs := c.certSignerForSigner(ctx, signer, filepath.Join(t.TempDir(), "id_ed25519"))

	require.NotNil(t, cs, "unexpanded CertificateFile tilde path must still resolve")
}

// TestCertSignerForSignerMismatch verifies that a cert whose key does not match
// the signer is silently skipped.
func TestCertSignerForSignerMismatch(t *testing.T) {
	ctx := context.Background()
	_, privA, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signerA, err := ssh.NewSignerFromKey(privA)
	require.NoError(t, err)

	_, privB, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signerB, err := ssh.NewSignerFromKey(privB)
	require.NoError(t, err)

	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519")
	// cert is for key B, but signer is A
	cert := makeCertForSigner(t, signerB)
	writeCert(t, keyPath+"-cert.pub", cert)

	c := &Connection{sshConfig: &sshconfig.Config{}}
	cs := c.certSignerForSigner(ctx, signerA, keyPath)
	require.Nil(t, cs, "mismatched cert must be skipped")
}

// TestCertSignerForSignerMissingFile verifies that a missing cert file is
// silently skipped without error.
func TestCertSignerForSignerMissingFile(t *testing.T) {
	ctx := context.Background()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(priv)
	require.NoError(t, err)

	c := &Connection{sshConfig: &sshconfig.Config{}}
	cs := c.certSignerForSigner(ctx, signer, filepath.Join(t.TempDir(), "id_ed25519"))
	require.Nil(t, cs, "missing cert file must return nil without error")
}

// TestCertSignerForSignerNoneDisablesImplicit verifies that a literal "none"
// entry in CertificateFile disables the implicit <keyPath>-cert.pub fallback.
// This covers the programmatic / pre-Finalize case: sshconfig.Setter.Finalize()
// normalizes a lone ["none"] to nil, so parser-loaded configs reach this code
// with a nil slice instead (and the implicit path is tried as normal).
func TestCertSignerForSignerNoneDisablesImplicit(t *testing.T) {
	ctx := context.Background()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(priv)
	require.NoError(t, err)

	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519")
	cert := makeCertForSigner(t, signer)
	writeCert(t, keyPath+"-cert.pub", cert)

	c := &Connection{sshConfig: &sshconfig.Config{CertificateFile: []string{"none"}}}
	cs := c.certSignerForSigner(ctx, signer, keyPath)
	require.Nil(t, cs, "CertificateFile=none must disable implicit cert loading")
}

// TestCertSignerForSignerSkipsHostCert verifies that host certificates are
// skipped and not offered as user authentication.
func TestCertSignerForSignerSkipsHostCert(t *testing.T) {
	ctx := context.Background()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(priv)
	require.NoError(t, err)

	_, caPriv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	caSigner, err := ssh.NewSignerFromKey(caPriv)
	require.NoError(t, err)

	hostCert := &ssh.Certificate{
		Key:         signer.PublicKey(),
		CertType:    ssh.HostCert,
		KeyId:       "host",
		ValidAfter:  0,
		ValidBefore: ssh.CertTimeInfinity,
	}
	require.NoError(t, hostCert.SignCert(rand.Reader, caSigner))

	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519")
	writeCert(t, keyPath+"-cert.pub", hostCert)

	c := &Connection{sshConfig: &sshconfig.Config{}}
	cs := c.certSignerForSigner(ctx, signer, keyPath)
	require.Nil(t, cs, "host certificate must be skipped")
}

// TestLoadKeySignersIncludesCertSigner verifies that loadKeySigners prepends
// the cert signer before the plain signer when a certificate is available.
func TestLoadKeySignersIncludesCertSigner(t *testing.T) {
	ctx := context.Background()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(priv)
	require.NoError(t, err)

	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519")

	privBlock, err := ssh.MarshalPrivateKey(priv, "")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(keyPath, pem.EncodeToMemory(privBlock), 0o600))

	cert := makeCertForSigner(t, signer)
	writeCert(t, keyPath+"-cert.pub", cert)

	signerCache.Delete(keyPath)
	t.Cleanup(func() { signerCache.Delete(keyPath) })

	c := &Connection{sshConfig: &sshconfig.Config{}, keyPaths: []string{keyPath}}

	signers := c.loadKeySigners(ctx, nil)
	require.Len(t, signers, 2, "must have cert signer and plain signer")

	_, isCert := signers[0].PublicKey().(*ssh.Certificate)
	require.True(t, isCert, "first signer must be the cert signer (cert takes priority)")
	_, isCert = signers[1].PublicKey().(*ssh.Certificate)
	require.False(t, isCert, "second signer must be the plain signer")
}
