package rig

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	ssh "golang.org/x/crypto/ssh"
)

// setAgentSigners replaces agentSignerSource for the duration of a test.
func setAgentSigners(t *testing.T, signers []ssh.Signer) {
	t.Helper()
	orig := agentSignerSource
	agentSignerSource = func() ([]ssh.Signer, error) { return signers, nil }
	t.Cleanup(func() { agentSignerSource = orig })
}

// makeSigner creates an in-memory ed25519 signer (no file I/O).
func makeSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	s, err := ssh.NewSignerFromKey(priv)
	require.NoError(t, err)
	return s
}

// generateKeyFile writes a fresh ed25519 private key to a temp file and returns its path.
func generateKeyFile(t *testing.T, dir string, name string) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	block, err := ssh.MarshalPrivateKey(priv, "")
	require.NoError(t, err)
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(block), 0o600))
	return path
}

// TestClientConfigSinglePublickeyAuthMethod verifies that multiple identity files
// are combined into a single publickey AuthMethod. x/crypto/ssh only tries the
// first AuthMethod of a given type per connection attempt, so bundling all signers
// is required for servers that reject the first key but would accept a later one.
func TestClientConfigSinglePublickeyAuthMethod(t *testing.T) {
	// SSH_KNOWN_HOSTS="" triggers insecure host key acceptance (no file needed).
	t.Setenv("SSH_KNOWN_HOSTS", "")

	tmpDir := t.TempDir()

	var keyPaths []string
	for i := 0; i < 3; i++ {
		keyPaths = append(keyPaths, generateKeyFile(t, tmpDir, fmt.Sprintf("id_ed25519_%d", i)))
	}

	c := &SSH{
		Address:  "127.0.0.1",
		User:     "root",
		keyPaths: keyPaths,
	}

	cfg, err := c.clientConfig()
	require.NoError(t, err)

	// All signers must be bundled into exactly one publickey AuthMethod.
	require.Len(t, cfg.Auth, 1, "all key-file signers must be combined into a single publickey AuthMethod")
}

// TestMergeSignersOrdering verifies that mergeSigners places key-file signers
// before agent signers. On servers with a low MaxAuthTries limit an explicitly
// configured key must be tried before any agent keys.
func TestMergeSignersOrdering(t *testing.T) {
	fileKey1 := makeSigner(t)
	fileKey2 := makeSigner(t)
	agentKey := makeSigner(t)

	result := mergeSigners([]ssh.Signer{fileKey1, fileKey2}, []ssh.Signer{agentKey})

	require.Len(t, result, 3)
	require.Equal(t, fileKey1.PublicKey().Marshal(), result[0].PublicKey().Marshal(), "first file key must come first")
	require.Equal(t, fileKey2.PublicKey().Marshal(), result[1].PublicKey().Marshal(), "second file key must come second")
	require.Equal(t, agentKey.PublicKey().Marshal(), result[2].PublicKey().Marshal(), "agent key must come last")
}

// TestMergeSignersDeduplicate verifies that when a key-file signer has the same
// public key as an agent signer, it appears only once (key-file copy is kept).
// Duplicate offers waste MaxAuthTries and can cause premature connection failure.
func TestMergeSignersDeduplicate(t *testing.T) {
	shared := makeSigner(t)
	agentOnly := makeSigner(t)

	result := mergeSigners([]ssh.Signer{shared}, []ssh.Signer{shared, agentOnly})

	require.Len(t, result, 2, "shared key must be deduplicated")
	require.Equal(t, shared.PublicKey().Marshal(), result[0].PublicKey().Marshal(), "shared key must be first (file copy)")
	require.Equal(t, agentOnly.PublicKey().Marshal(), result[1].PublicKey().Marshal(), "agent-only key must be second")
}

// TestClientConfigProducesSinglePublickeyAuthMethod verifies that clientConfig
// combines key-file and agent signers into exactly one publickey AuthMethod,
// regardless of how many signers are present.
func TestClientConfigProducesSinglePublickeyAuthMethod(t *testing.T) {
	t.Setenv("SSH_KNOWN_HOSTS", "")

	tmpDir := t.TempDir()
	agentSigner := makeSigner(t)

	keyPaths := []string{
		generateKeyFile(t, tmpDir, "key0"),
		generateKeyFile(t, tmpDir, "key1"),
	}

	setAgentSigners(t, []ssh.Signer{agentSigner})

	c := &SSH{
		Address:  "127.0.0.1",
		User:     "root",
		keyPaths: keyPaths,
	}

	cfg, err := c.clientConfig()
	require.NoError(t, err)
	require.Len(t, cfg.Auth, 1, "key-file and agent signers must be combined into one publickey AuthMethod")
}

// TestClientConfigSharedKeyIsDeduplicatedWithAgent verifies that clientConfig
// produces a single publickey AuthMethod even when the same key appears both in
// a key file and in the agent (deduplication via mergeSigners).
func TestClientConfigSharedKeyIsDeduplicatedWithAgent(t *testing.T) {
	t.Setenv("SSH_KNOWN_HOSTS", "")

	tmpDir := t.TempDir()

	_, sharedPriv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	block, err := ssh.MarshalPrivateKey(sharedPriv, "")
	require.NoError(t, err)
	sharedKeyPath := filepath.Join(tmpDir, "shared_key")
	require.NoError(t, os.WriteFile(sharedKeyPath, pem.EncodeToMemory(block), 0o600))
	sharedSigner, err := ssh.NewSignerFromKey(sharedPriv)
	require.NoError(t, err)
	agentOnlySigner := makeSigner(t)

	// sharedSigner appears both as a file key and an agent key.
	setAgentSigners(t, []ssh.Signer{sharedSigner, agentOnlySigner})

	c := &SSH{
		Address:  "127.0.0.1",
		User:     "root",
		keyPaths: []string{sharedKeyPath},
	}

	cfg, err := c.clientConfig()
	require.NoError(t, err)
	// One auth method is produced; mergeSigners deduplication is exercised
	// because the shared key would produce two entries without it.
	require.Len(t, cfg.Auth, 1)
}

// TestClientConfigExplicitAuthMethodsAreUsedExclusively verifies that when the
// caller provides explicit AuthMethods, those are used as-is and key-path
// processing is skipped. This avoids injecting a competing publickey AuthMethod
// alongside a caller-supplied one (x/crypto/ssh only tries the first method of
// each type per connection attempt).
func TestClientConfigExplicitAuthMethodsAreUsedExclusively(t *testing.T) {
	t.Setenv("SSH_KNOWN_HOSTS", "")

	tmpDir := t.TempDir()
	keyPath := generateKeyFile(t, tmpDir, "id_ed25519")

	sentinel := ssh.Password("test-password")
	c := &SSH{
		Address:     "127.0.0.1",
		User:        "root",
		keyPaths:    []string{keyPath},
		AuthMethods: []ssh.AuthMethod{sentinel},
	}

	cfg, err := c.clientConfig()
	require.NoError(t, err)

	// Verify the count matches exactly: if key-path processing were not skipped,
	// a second (publickey) method would be added for the key file, making len > 1.
	require.Len(t, cfg.Auth, len(c.AuthMethods), "only the caller-supplied AuthMethods should be present; no extra methods added")
}
