package ssh

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/k0sproject/rig/v2/protocol"
	"github.com/k0sproject/rig/v2/protocol/ssh/hostkey"
	"github.com/k0sproject/rig/v2/sshconfig"
	"github.com/k0sproject/rig/v2/sshconfig/options"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ssh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// errAuthRejected is what test SSH servers return when refusing a credential.
var errAuthRejected = errors.New("auth rejected")

// startSSHServer starts an in-process SSH server on a random loopback port using
// the given config and returns its listen address. It serves no channels: every
// channel-open request is rejected. The listener is closed by t.Cleanup.
func startSSHServer(t *testing.T, cfg *ssh.ServerConfig) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, lErr := ln.Accept()
			if lErr != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				sconn, chans, reqs, hsErr := ssh.NewServerConn(c, cfg)
				if hsErr != nil {
					return
				}
				defer sconn.Close()
				go ssh.DiscardRequests(reqs)
				for newChan := range chans {
					newChan.Reject(ssh.UnknownChannelType, "not supported") //nolint:errcheck
				}
			}(conn)
		}
	}()

	return ln.Addr().String()
}

// newHostSigner generates an ephemeral ed25519 host key for a test SSH server.
func newHostSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(priv)
	require.NoError(t, err)
	return signer
}

// pinHostKey writes a known_hosts file pinning hostSigner's public key for addr
// and points SSH_KNOWN_HOSTS at it, so host key verification is exercised rather
// than bypassed. knownhosts.Normalize converts "host:port" to "[host]:port" for
// non-22 ports, which is the form the validator looks up.
func pinHostKey(t *testing.T, addr string, hostSigner ssh.Signer) {
	t.Helper()
	line := fmt.Sprintf("%s %s\n",
		knownhosts.Normalize(addr),
		strings.TrimRight(string(ssh.MarshalAuthorizedKey(hostSigner.PublicKey())), "\n"),
	)
	path := filepath.Join(t.TempDir(), "known_hosts")
	require.NoError(t, os.WriteFile(path, []byte(line), 0o600))
	t.Setenv("SSH_KNOWN_HOSTS", path)
}

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
		Address: "127.0.0.1",
		User:    "test",
		Port:    22,
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

func Test_isAuthError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "handshake exhausted all methods",
			err:  errors.New("ssh: handshake failed: ssh: unable to authenticate, attempted methods [none publickey], no supported methods remain"),
			want: true,
		},
		{
			name: "wrapped handshake failure",
			err:  fmt.Errorf("ssh dial: %w", errors.New("ssh: unable to authenticate, attempted methods [none], no supported methods remain")),
			want: true,
		},
		{
			name: "connection refused",
			err:  errors.New("dial tcp 10.0.0.1:22: connect: connection refused"),
			want: false,
		},
		{
			name: "host key mismatch",
			err:  errors.New("ssh: handshake failed: host key mismatch"),
			want: false,
		},
		{
			name: "remote command permission denied",
			err:  errors.New("Process exited with status 1: cat: /etc/shadow: Permission denied"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, isAuthError(tt.err), "isAuthError(%v)", tt.err)
		})
	}
}

// TestConnectAuthFailure drives Connect against an in-process SSH server that
// refuses every credential it is offered. The resulting error must be tagged
// protocol.ErrAuthFailed so callers can fail fast on bad credentials, but must
// not be non-retryable: the same credentials can start working once the remote
// finishes provisioning.
func TestConnectAuthFailure(t *testing.T) {
	withConfigParser(t, "")
	t.Setenv("SSH_AUTH_SOCK", "")

	hostSigner := newHostSigner(t)
	cfg := &ssh.ServerConfig{
		ServerVersion: "SSH-2.0-test-linux",
		PasswordCallback: func(_ ssh.ConnMetadata, _ []byte) (*ssh.Permissions, error) {
			return nil, errAuthRejected
		},
	}
	cfg.AddHostKey(hostSigner)

	serverAddr := startSSHServer(t, cfg)
	pinHostKey(t, serverAddr, hostSigner)

	serverHost, serverPortStr, err := net.SplitHostPort(serverAddr)
	require.NoError(t, err)
	serverPort, err := strconv.Atoi(serverPortStr)
	require.NoError(t, err)

	conn, err := NewConnection(Config{
		Address:     serverHost,
		Port:        serverPort,
		User:        "test",
		AuthMethods: []ssh.AuthMethod{ssh.Password("wrong")},
	})
	require.NoError(t, err)
	t.Cleanup(conn.Disconnect)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = conn.Connect(ctx)
	require.Error(t, err, "Connect must fail against a server that rejects every credential")
	require.ErrorIs(t, err, protocol.ErrAuthFailed)
	require.NotErrorIs(t, err, protocol.ErrNonRetryable,
		"a credential rejection can clear once the remote finishes provisioning")
	require.ErrorContains(t, err, "ssh dial",
		"the tag must come from the direct-connect handshake, not some other path")
	require.False(t, conn.IsConnected())
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

	cb, err := c.hostkeyCallback(context.Background(), false)
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

	cb, err := c.hostkeyCallback(context.Background(), false)
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

	cb, err := c.hostkeyCallback(context.Background(), false)
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

	_, err := c.hostkeyCallback(context.Background(), false)
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

	_, err := c.hostkeyCallback(context.Background(), false)
	require.Error(t, err, "missing global known_hosts must not be created — should fall through to error")

	_, statErr := os.Stat(missing)
	require.True(t, os.IsNotExist(statErr), "hostkeyCallback must not create missing global known_hosts files")
}

func TestHostkeyCallbackCheckHostIPEnabled(t *testing.T) {
	unsetKnownHostsEnv(t)

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(priv)
	require.NoError(t, err)

	// Write known_hosts with a single IP entry — the callback is exercised
	// with that IP as hostname so WithCheckHostIP skips the DNS lookup.
	addr, err := net.ResolveTCPAddr("tcp", "192.0.2.1:22")
	require.NoError(t, err)
	line := string(ssh.MarshalAuthorizedKey(signer.PublicKey()))
	khPath := filepath.Join(t.TempDir(), "known_hosts")
	require.NoError(t, os.WriteFile(khPath,
		[]byte("[192.0.2.1]:22 "+line),
		0o600))

	c := &Connection{
		sshConfig: &sshconfig.Config{
			UserKnownHostsFile: []string{khPath},
			CheckHostIP:        options.BooleanOption("yes"),
		},
	}

	cb, err := c.hostkeyCallback(context.Background(), true)
	require.NoError(t, err)
	require.NotNil(t, cb)

	// IP hostname: WithCheckHostIP must skip DNS and accept the known key.
	require.NoError(t, cb("192.0.2.1:22", addr, signer.PublicKey()))
}

// TestHostkeyCallbackHostKeyAliasDisablesCheckHostIP verifies that when
// HostKeyAlias is set, IP verification (CheckHostIP) is suppressed even when
// CheckHostIP is also enabled. A mismatching key stored under the real IP
// in known_hosts must not cause ErrHostKeyMismatch.
func TestHostkeyCallbackHostKeyAliasDisablesCheckHostIP(t *testing.T) {
	unsetKnownHostsEnv(t)

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(priv)
	require.NoError(t, err)

	// spoofSigner represents a different key stored under the real IP address —
	// the entry that CheckHostIP would flag as a mismatch when both are present.
	_, spoofPriv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	spoofSigner, err := ssh.NewSignerFromKey(spoofPriv)
	require.NoError(t, err)

	// Populate known_hosts with the correct key under the alias and a DIFFERENT
	// key under the actual TCP IP to simulate what CheckHostIP would detect.
	aliasEntry := knownhosts.Line([]string{knownhosts.Normalize("alias-host:22")}, signer.PublicKey())
	ipEntry := knownhosts.Line([]string{knownhosts.Normalize("192.0.2.1:22")}, spoofSigner.PublicKey())
	khPath := filepath.Join(t.TempDir(), "known_hosts")
	require.NoError(t, os.WriteFile(khPath, []byte(aliasEntry+"\n"+ipEntry+"\n"), 0o600))

	c := &Connection{
		sshConfig: &sshconfig.Config{
			UserKnownHostsFile: []string{khPath},
			CheckHostIP:        options.BooleanOption("yes"),
			HostKeyAlias:       "alias-host",
		},
	}

	cb, err := c.hostkeyCallback(context.Background(), false)
	require.NoError(t, err)

	// Wrap with alias as clientConfig does.
	cb = hostkey.WithAlias(cb, "alias-host")

	addr, err := net.ResolveTCPAddr("tcp", "192.0.2.1:22")
	require.NoError(t, err)

	// Must succeed: alias lookup finds the correct key; IP check is suppressed.
	require.NoError(t, cb("192.0.2.1:22", addr, signer.PublicKey()))
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

// TestIgnoreSSHConfig covers the opt-out from ~/.ssh/config and the
// system-wide ssh_config. See https://github.com/k0sproject/rig/issues/444.
func TestIgnoreSSHConfig(t *testing.T) {
	// A directive that the parser rejects, in a stanza that has nothing to do
	// with the host being connected to. This is the shape reported in #444:
	// an unrelated include file makes every connection fail.
	const brokenConfig = "Match bogus \"x\"\n  Port 2222\n"

	// A parseable config that changes a value rig reads, used to verify that
	// file-derived settings really are dropped.
	const portConfig = "Host *\n  Port 2222\n"

	t.Run("broken config fails without the opt-out", func(t *testing.T) {
		withConfigParser(t, brokenConfig)
		_, err := NewConnection(Config{Address: "192.0.2.10", User: "test"})
		require.ErrorContains(t, err, "invalid Match condition")
	})

	t.Run("the parse failure is reported without stuttering", func(t *testing.T) {
		// sshconfig.Parser.Apply already prefixes its errors with "failed to
		// apply ssh config"; wrapping it again here is what produced the
		// doubled sentence quoted in the bug report.
		withConfigParser(t, brokenConfig)
		_, err := NewConnection(Config{Address: "192.0.2.10", User: "test"})
		require.Error(t, err)
		require.Equal(t, 1, strings.Count(err.Error(), "failed to apply ssh config"),
			"the phrase should appear exactly once, got: %s", err)
	})

	t.Run("broken config is bypassed with the opt-out", func(t *testing.T) {
		withConfigParser(t, brokenConfig)
		conn, err := NewConnection(Config{Address: "192.0.2.10", User: "test", IgnoreSSHConfig: true})
		require.NoError(t, err)
		require.NotNil(t, conn.sshConfig)
	})

	t.Run("broken config is bypassed with WithoutSSHConfig", func(t *testing.T) {
		withConfigParser(t, brokenConfig)
		conn, err := NewConnection(Config{Address: "192.0.2.10", User: "test"}, WithoutSSHConfig())
		require.NoError(t, err)
		require.True(t, conn.IgnoreSSHConfig)
	})

	t.Run("file derived settings are dropped", func(t *testing.T) {
		withConfigParser(t, portConfig)

		conn, err := NewConnection(Config{Address: "192.0.2.10", User: "test"})
		require.NoError(t, err)
		require.Equal(t, 2222, conn.Port, "sanity: the config file should set the port")

		ignored, err := NewConnection(Config{Address: "192.0.2.10", User: "test", IgnoreSSHConfig: true})
		require.NoError(t, err)
		require.Equal(t, 22, ignored.Port, "port must fall back to the built-in default")
	})

	t.Run("built-in defaults are still applied", func(t *testing.T) {
		withConfigParser(t, brokenConfig)
		conn, err := NewConnection(Config{Address: "192.0.2.10", User: "test", IgnoreSSHConfig: true})
		require.NoError(t, err)

		// These all come from OpenSSH's compiled-in defaults rather than from a
		// config file, so ignoring the files must not drop them.
		require.Equal(t, 22, conn.sshConfig.Port)
		require.NotEmpty(t, conn.sshConfig.IdentityFile, "default identity files must survive")
		require.NotEmpty(t, conn.sshConfig.UserKnownHostsFile, "default known_hosts must survive")
		require.NotEmpty(t, conn.sshConfig.StrictHostKeyChecking, "default StrictHostKeyChecking must survive")
		require.NotEmpty(t, conn.sshConfig.Ciphers, "default ciphers must survive")

		// Defaults are token-expanded by the parser's finalize step.
		for _, f := range conn.sshConfig.IdentityFile {
			require.NotContains(t, f, "~", "identity file paths must be expanded")
		}
	})

	t.Run("defaults-only parser is always available", func(t *testing.T) {
		parser, err := defaultsOnlyConfigParser()
		require.NoError(t, err)
		require.NotNil(t, parser, "the opt-out must never fall through to applying nothing")
	})

	t.Run("defaults are applied even when ConfigParser is nil", func(t *testing.T) {
		// A nil ConfigParser means "apply nothing" for the normal path, but the
		// opt-out promises to keep the built-in defaults, so it must not share
		// that fate.
		prev := ConfigParser
		ConfigParser = nil
		t.Cleanup(func() { ConfigParser = prev })

		conn, err := NewConnection(Config{Address: "192.0.2.10", User: "test", IgnoreSSHConfig: true})
		require.NoError(t, err)
		require.Equal(t, 22, conn.sshConfig.Port)
		require.NotEmpty(t, conn.sshConfig.IdentityFile)
		require.NotEmpty(t, conn.sshConfig.UserKnownHostsFile)
	})

	t.Run("explicit options still apply", func(t *testing.T) {
		withConfigParser(t, brokenConfig)
		conn, err := NewConnection(Config{
			Address:          "192.0.2.10",
			User:             "test",
			IgnoreSSHConfig:  true,
			SSHConfigOptions: sshconfig.OptionArguments{"ConnectTimeout": "42s"},
		})
		require.NoError(t, err)
		require.Equal(t, 42*time.Second, conn.sshConfig.ConnectTimeout)
	})

	t.Run("bastion inherits the opt-out", func(t *testing.T) {
		withConfigParser(t, brokenConfig)
		conn, err := NewConnection(Config{
			Address:         "192.0.2.10",
			User:            "test",
			IgnoreSSHConfig: true,
			Bastion:         &Config{Address: "192.0.2.20", User: "test"},
		})
		require.NoError(t, err)
		require.True(t, conn.Bastion.IgnoreSSHConfig)

		// The bastion builds its own Connection, which would otherwise trip on
		// the same broken config.
		bastionConn, err := conn.Bastion.Connection()
		require.NoError(t, err)
		require.NotNil(t, bastionConn)
	})

	t.Run("proxyjump bastion inherits the opt-out", func(t *testing.T) {
		withConfigParser(t, brokenConfig)
		conn, err := NewConnection(Config{
			Address:          "192.0.2.10",
			User:             "test",
			IgnoreSSHConfig:  true,
			SSHConfigOptions: sshconfig.OptionArguments{"ProxyJump": "jump.example.com"},
		})
		require.NoError(t, err)
		require.NotNil(t, conn.Bastion, "ProxyJump from options must still wire a bastion")
		require.True(t, conn.Bastion.IgnoreSSHConfig)
	})
}

// TestConfigParserOrDefaults covers what ConfigParser is initialized to when
// the ssh config files cannot be parsed at all. A syntax error there must not
// also cost the connection OpenSSH's built-in defaults.
func TestConfigParserOrDefaults(t *testing.T) {
	t.Run("a parsed config is used as-is", func(t *testing.T) {
		parser, err := sshconfig.NewParser(strings.NewReader("Host *\n  Port 2222\n"))
		require.NoError(t, err)
		require.Same(t, parser, configParserOrDefaults(parser, nil))
	})

	t.Run("an unparseable config falls back to the built-in defaults", func(t *testing.T) {
		// This is what a stray line in ~/.ssh/config does: NewParser fails
		// outright, so there is no parser to apply.
		_, err := sshconfig.NewParser(strings.NewReader("garbageline\n"))
		require.Error(t, err, "sanity: a line with no separator must fail to parse")

		parser := configParserOrDefaults(nil, err)
		require.NotNil(t, parser, "the built-in defaults must survive an unparseable config file")

		cfg := &sshconfig.Config{}
		require.NoError(t, parser.Apply(cfg, "192.0.2.10"))
		require.Equal(t, 22, cfg.Port)
		require.NotEmpty(t, cfg.IdentityFile, "default identity files must survive")
		require.NotEmpty(t, cfg.UserKnownHostsFile, "default known_hosts must survive")
	})

	t.Run("the fallback does not read the config files", func(t *testing.T) {
		parser := configParserOrDefaults(nil, errors.New("broken"))
		require.NotNil(t, parser)

		cfg := &sshconfig.Config{}
		require.NoError(t, parser.Apply(cfg, "192.0.2.10"))
		// 2222 could only come from a config file; the fallback must not read one.
		require.Equal(t, 22, cfg.Port)
	})
}

// capturingLogger records the messages written to it so a test can assert on
// which one was chosen.
type capturingLogger struct {
	debug []string
	warn  []string
}

func (l *capturingLogger) Debug(msg string, _ ...any) { l.debug = append(l.debug, msg) }
func (l *capturingLogger) Info(msg string, _ ...any)  { _ = msg }
func (l *capturingLogger) Warn(msg string, _ ...any)  { l.warn = append(l.warn, msg) }
func (l *capturingLogger) Error(msg string, _ ...any) { _ = msg }

// TestLogConfigSource pins what the connection says it is applying. The files
// are not always part of it, and a log that claims otherwise is misleading in
// exactly the situation someone reads it to debug.
//
// The parser is passed in rather than read from the global, so each case is a
// state the process can really be in: the warning belongs to the defaults-only
// fallback being applied, not merely to a parse error having been recorded at
// startup.
func TestLogConfigSource(t *testing.T) {
	setParseErr := func(t *testing.T, err error) {
		t.Helper()
		prev := errConfigParse
		errConfigParse = err
		t.Cleanup(func() { errConfigParse = prev })
	}
	newConn := func(ignore bool) (*Connection, *capturingLogger) {
		logger := &capturingLogger{}
		conn := &Connection{}
		conn.IgnoreSSHConfig = ignore
		conn.SetLogger(logger)
		return conn, logger
	}
	fileParser := func(t *testing.T) *sshconfig.Parser {
		t.Helper()
		parser, err := sshconfig.NewParser(strings.NewReader("Host *\n  Port 2222\n"))
		require.NoError(t, err)
		return parser
	}
	fallbackParser := func(t *testing.T) *sshconfig.Parser {
		t.Helper()
		parser, err := defaultsOnlyConfigParser()
		require.NoError(t, err)
		return parser
	}

	t.Run("normal path names the files and the defaults", func(t *testing.T) {
		setParseErr(t, nil)
		conn, logger := newConn(false)

		conn.logConfigSource(fileParser(t))
		require.Equal(t, []string{"applying ssh config files and built-in defaults"}, logger.debug)
		require.Empty(t, logger.warn)
	})

	t.Run("opt-out says the files are skipped", func(t *testing.T) {
		setParseErr(t, nil)
		conn, logger := newConn(true)

		conn.logConfigSource(fallbackParser(t))
		require.Equal(t, []string{"ignoring ssh config files, applying built-in defaults only"}, logger.debug)
		require.Empty(t, logger.warn)
	})

	t.Run("the fallback in use warns instead of claiming the files were read", func(t *testing.T) {
		setParseErr(t, errors.New("syntax error: missing separator"))
		conn, logger := newConn(false)

		conn.logConfigSource(fallbackParser(t))
		require.Empty(t, logger.debug, "this must not be reported as a normal application of the files")
		require.Equal(t, []string{"ssh config files could not be parsed, applying built-in defaults only"}, logger.warn,
			"the user wrote settings that are being ignored, so it should be visible")
	})

	t.Run("a replaced parser stops the warning", func(t *testing.T) {
		// errConfigParse is set once at startup and never cleared, so keying the
		// warning on it alone would keep blaming a fallback that is no longer
		// the parser being applied. Overriding ConfigParser is a supported thing
		// to do, and the tests here do it themselves.
		setParseErr(t, errors.New("syntax error: missing separator"))
		conn, logger := newConn(false)

		conn.logConfigSource(fileParser(t))
		require.Empty(t, logger.warn, "the fallback is not what is being applied any more")
		require.Equal(t, []string{"applying ssh config files and built-in defaults"}, logger.debug)
	})

	t.Run("the opt-out takes precedence over a parse failure", func(t *testing.T) {
		setParseErr(t, errors.New("syntax error: missing separator"))
		conn, logger := newConn(true)

		conn.logConfigSource(fallbackParser(t))
		require.Empty(t, logger.warn, "the files were not going to be read anyway")
	})
}

// TestPropagatePort pins the port handling and what it claims in the log. Port
// 22 is deliberately treated as unset so the ssh config files can still supply
// one, which makes the log line wrong once those files are skipped.
func TestPropagatePort(t *testing.T) {
	newConn := func(port int, ignore bool) (*Connection, *capturingLogger) {
		logger := &capturingLogger{}
		conn := &Connection{sshConfig: &sshconfig.Config{}}
		conn.Port = port
		conn.IgnoreSSHConfig = ignore
		conn.SetLogger(logger)
		return conn, logger
	}

	t.Run("an explicit port is propagated", func(t *testing.T) {
		conn, logger := newConn(2222, false)
		conn.propagatePort()
		require.Equal(t, 2222, conn.sshConfig.Port)
		require.Equal(t, []string{"propagating explicit port to ssh config"}, logger.debug)
	})

	t.Run("port 22 defers to the config files", func(t *testing.T) {
		conn, logger := newConn(22, false)
		conn.propagatePort()
		require.Zero(t, conn.sshConfig.Port, "22 must stay unset so the config files can supply a port")
		require.Equal(t, []string{"port is default (22) — deferring to the ssh config files"}, logger.debug)
	})

	t.Run("port 22 with the opt-out does not claim to defer to anything", func(t *testing.T) {
		conn, logger := newConn(22, true)
		conn.propagatePort()
		require.Zero(t, conn.sshConfig.Port)
		require.Equal(t, []string{"port is default (22) — ssh config files ignored, keeping it"}, logger.debug)
		require.NotContains(t, logger.debug[0], "deferring",
			"there are no config files to defer to when they are skipped")
	})
}
