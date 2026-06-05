package hostkey_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/k0sproject/rig/v2/protocol/ssh/hostkey"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func newTestSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(priv)
	require.NoError(t, err)
	return signer
}

func TestKnownHostsReadOnlyFileCallbackDoesNotCreateFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "ssh_known_hosts")

	_, err := hostkey.KnownHostsReadOnlyFileCallback(missing, false)
	require.Error(t, err, "read-only callback must fail for a missing file, not create it")

	_, statErr := os.Stat(missing)
	require.True(t, os.IsNotExist(statErr), "read-only callback must not create the file")
}

func TestKnownHostsReadOnlyFileCallbackDoesNotAppendUnknownHost(t *testing.T) {
	dir := t.TempDir()
	khFile := filepath.Join(dir, "ssh_known_hosts")
	require.NoError(t, os.WriteFile(khFile, []byte(""), 0o644))

	cb, err := hostkey.KnownHostsReadOnlyFileCallback(khFile, false)
	require.NoError(t, err)

	signer := newTestSigner(t)
	addr, err := net.ResolveTCPAddr("tcp", "192.0.2.1:22")
	require.NoError(t, err)

	cbErr := cb("192.0.2.1:22", addr, signer.PublicKey())
	require.Error(t, cbErr, "unknown host must be rejected in strict mode")

	contents, err := os.ReadFile(khFile)
	require.NoError(t, err)
	require.Empty(t, contents, "read-only callback must not append to the file")
}

func TestKnownHostsReadOnlyFileCallbackPermissiveAcceptsUnknownHost(t *testing.T) {
	dir := t.TempDir()
	khFile := filepath.Join(dir, "ssh_known_hosts")
	require.NoError(t, os.WriteFile(khFile, []byte(""), 0o644))

	cb, err := hostkey.KnownHostsReadOnlyFileCallback(khFile, true)
	require.NoError(t, err)

	signer := newTestSigner(t)
	addr, err := net.ResolveTCPAddr("tcp", "192.0.2.1:22")
	require.NoError(t, err)

	require.NoError(t, cb("192.0.2.1:22", addr, signer.PublicKey()), "permissive mode must accept unknown host without error")

	contents, err := os.ReadFile(khFile)
	require.NoError(t, err)
	require.Empty(t, contents, "permissive read-only callback must not append to the file")
}

func TestKnownHostsReadOnlyFileCallbackUnknownHostIsHostKeyMismatch(t *testing.T) {
	dir := t.TempDir()
	khFile := filepath.Join(dir, "ssh_known_hosts")
	require.NoError(t, os.WriteFile(khFile, []byte(""), 0o644))

	cb, err := hostkey.KnownHostsReadOnlyFileCallback(khFile, false)
	require.NoError(t, err)

	signer := newTestSigner(t)
	addr, err := net.ResolveTCPAddr("tcp", "192.0.2.1:22")
	require.NoError(t, err)

	cbErr := cb("192.0.2.1:22", addr, signer.PublicKey())
	require.ErrorIs(t, cbErr, hostkey.ErrHostKeyMismatch, "unknown host in read-only strict mode must return ErrHostKeyMismatch so callers treat it as non-retryable")
}

func TestKnownHostsReadOnlyFileCallbackAcceptsKnownHost(t *testing.T) {
	signer := newTestSigner(t)

	dir := t.TempDir()
	khFile := filepath.Join(dir, "ssh_known_hosts")
	line := knownhosts.Line([]string{knownhosts.Normalize("192.0.2.1:22")}, signer.PublicKey())
	require.NoError(t, os.WriteFile(khFile, []byte(line+"\n"), 0o644))

	cb, err := hostkey.KnownHostsReadOnlyFileCallback(khFile, false)
	require.NoError(t, err)

	addr, err := net.ResolveTCPAddr("tcp", "192.0.2.1:22")
	require.NoError(t, err)
	require.NoError(t, cb("192.0.2.1:22", addr, signer.PublicKey()))
}
