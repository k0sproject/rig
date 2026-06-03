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

	"github.com/k0sproject/rig/v2/protocol"
	"github.com/k0sproject/rig/v2/sshconfig"
	"github.com/k0sproject/rig/v2/sshconfig/options"
	"github.com/stretchr/testify/require"
	ssh "golang.org/x/crypto/ssh"
)

// newTestConnection builds a Connection with passed-in auth methods (to bypass
// key/agent loading) and an empty SSH_KNOWN_HOSTS env (to take the
// InsecureIgnoreHostKey path in hostkeyCallback).
func newTestConnection(t *testing.T) *Connection {
	t.Helper()
	t.Setenv("SSH_KNOWN_HOSTS", "")

	// Replace the global ConfigParser with one backed by empty readers so
	// the developer's ~/.ssh/config and /etc/ssh/ssh_config don't bleed into
	// these tests.
	oldParser := ConfigParser
	emptyParser, err := sshconfig.NewParser(strings.NewReader(""))
	if err == nil {
		ConfigParser = emptyParser
	}
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

	_, err := c.pkeySigner(ctx, nil, path)
	require.Error(t, err)
	require.ErrorIs(t, err, protocol.ErrNonRetryable)
	require.NotContains(t, err.Error(), "can't parse keyfile",
		"BatchMode should short-circuit before the generic parse-failure path")
}

func TestPkeySignerEncryptedKeyWithoutBatchModeOrCallback(t *testing.T) {
	ctx := context.Background()
	c := newTestConnection(t)
	// BatchMode unset and no PasswordCallback: pkeySigner falls through to the
	// generic non-retryable parse error rather than the BatchMode message.
	c.PasswordCallback = nil

	path := writeEncryptedKey(t)

	_, err := c.pkeySigner(ctx, nil, path)
	require.Error(t, err)
	require.ErrorIs(t, err, protocol.ErrNonRetryable)
}

// TestPkeySignerBatchModeErrorNonCacheable guards against signer-cache poisoning:
// a BatchMode=yes connection must not permanently cache its "skip" error so that
// a later non-batch connection to the same key path still gets a chance to decrypt.
func TestPkeySignerBatchModeErrorNonCacheable(t *testing.T) {
	ctx := context.Background()
	c := newTestConnection(t)
	c.sshConfig.BatchMode = options.BooleanOption("yes")
	path := writeEncryptedKey(t)

	_, err := c.pkeySigner(ctx, nil, path)
	require.ErrorIs(t, err, errSkipCache, "batch-mode skip error must carry errSkipCache so clientConfig does not cache it")
}
