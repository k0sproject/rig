package ssh

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/k0sproject/rig/v2/protocol"
	"github.com/k0sproject/rig/v2/sshconfig"
	"github.com/k0sproject/rig/v2/sshconfig/options"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ssh "golang.org/x/crypto/ssh"
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
