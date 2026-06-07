package hostkey_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
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

// stubLookupHost overrides hostkey.LookupHostFunc for the duration of t and
// restores the original on cleanup.
func stubLookupHost(t *testing.T, fn func(string) ([]string, error)) {
	t.Helper()
	hostkey.LookupHostMu.Lock()
	prev := *hostkey.LookupHostFunc
	*hostkey.LookupHostFunc = fn
	hostkey.LookupHostMu.Unlock()
	t.Cleanup(func() {
		hostkey.LookupHostMu.Lock()
		*hostkey.LookupHostFunc = prev
		hostkey.LookupHostMu.Unlock()
	})
}

// writeKnownHostsFile writes the given lines to a temp known_hosts file and
// returns its path.
func writeKnownHostsFile(t *testing.T, lines ...string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "known_hosts")
	content := ""
	for _, l := range lines {
		content += l + "\n"
	}
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func TestWithCheckHostIPMismatchDetected(t *testing.T) {
	legit := newTestSigner(t) // key known_hosts records for the hostname
	spoof := newTestSigner(t) // different key stored for the IP → DNS spoofing

	khFile := writeKnownHostsFile(t,
		knownhosts.Line([]string{knownhosts.Normalize("example.com:22")}, legit.PublicKey()),
		knownhosts.Line([]string{knownhosts.Normalize("192.0.2.1:22")}, spoof.PublicKey()),
	)
	stubLookupHost(t, func(host string) ([]string, error) {
		if host == "example.com" {
			return []string{"192.0.2.1"}, nil
		}
		return nil, nil
	})

	inner, err := hostkey.KnownHostsReadOnlyFileCallback(khFile, false)
	require.NoError(t, err)

	wrapped, err := hostkey.WithCheckHostIP(inner, khFile, false)
	require.NoError(t, err)

	addr, err := net.ResolveTCPAddr("tcp", "192.0.2.1:22")
	require.NoError(t, err)

	cbErr := wrapped("example.com:22", addr, legit.PublicKey())
	require.ErrorIs(t, cbErr, hostkey.ErrHostKeyMismatch, "IP with different key must be detected as spoofing")
	require.Contains(t, cbErr.Error(), "192.0.2.1")
}

func TestWithCheckHostIPUnknownIPIsNonFatal(t *testing.T) {
	legit := newTestSigner(t)

	// known_hosts only has the hostname, not the IP
	khFile := writeKnownHostsFile(t,
		knownhosts.Line([]string{knownhosts.Normalize("example.com:22")}, legit.PublicKey()),
	)
	stubLookupHost(t, func(host string) ([]string, error) {
		return []string{"192.0.2.1"}, nil
	})

	inner, err := hostkey.KnownHostsReadOnlyFileCallback(khFile, false)
	require.NoError(t, err)

	wrapped, err := hostkey.WithCheckHostIP(inner, khFile, false)
	require.NoError(t, err)

	addr, err := net.ResolveTCPAddr("tcp", "192.0.2.1:22")
	require.NoError(t, err)

	require.NoError(t, wrapped("example.com:22", addr, legit.PublicKey()), "unknown IP must not be an error in detection-only mode")
}

func TestWithCheckHostIPSkipsWhenAlreadyIP(t *testing.T) {
	legit := newTestSigner(t)

	// Even if the IP has a different entry for another key, no lookup is done
	spoof := newTestSigner(t)
	khFile := writeKnownHostsFile(t,
		knownhosts.Line([]string{knownhosts.Normalize("192.0.2.1:22")}, spoof.PublicKey()),
	)

	lookupCalled := false
	stubLookupHost(t, func(host string) ([]string, error) {
		lookupCalled = true
		return nil, nil
	})

	inner, err := hostkey.KnownHostsReadOnlyFileCallback(khFile, true) // permissive so unknown hostname passes
	require.NoError(t, err)

	wrapped, err := hostkey.WithCheckHostIP(inner, khFile, false)
	require.NoError(t, err)

	addr, err := net.ResolveTCPAddr("tcp", "192.0.2.1:22")
	require.NoError(t, err)

	require.NoError(t, wrapped("192.0.2.1:22", addr, legit.PublicKey()))
	require.False(t, lookupCalled, "DNS lookup must be skipped when hostname is already an IP")
}

func TestWithCheckHostIPDNSFailureIsNonFatal(t *testing.T) {
	legit := newTestSigner(t)

	khFile := writeKnownHostsFile(t,
		knownhosts.Line([]string{knownhosts.Normalize("example.com:22")}, legit.PublicKey()),
	)
	stubLookupHost(t, func(host string) ([]string, error) {
		return nil, fmt.Errorf("simulated DNS failure")
	})

	inner, err := hostkey.KnownHostsReadOnlyFileCallback(khFile, false)
	require.NoError(t, err)

	wrapped, err := hostkey.WithCheckHostIP(inner, khFile, false)
	require.NoError(t, err)

	// Use a TCPAddr with nil IP so the DNS-resolution fallback is exercised
	// (a non-nil TCP IP would take the fast path and skip lookupHost).
	addr := &net.TCPAddr{Port: 22}

	require.NoError(t, wrapped("example.com:22", addr, legit.PublicKey()), "DNS failure must be non-fatal")
}

func TestWithCheckHostIPPermissiveDowngradesToWarning(t *testing.T) {
	legit := newTestSigner(t)
	spoof := newTestSigner(t)

	khFile := writeKnownHostsFile(t,
		knownhosts.Line([]string{knownhosts.Normalize("example.com:22")}, legit.PublicKey()),
		knownhosts.Line([]string{knownhosts.Normalize("192.0.2.1:22")}, spoof.PublicKey()),
	)
	stubLookupHost(t, func(host string) ([]string, error) {
		return []string{"192.0.2.1"}, nil
	})

	inner, err := hostkey.KnownHostsReadOnlyFileCallback(khFile, true)
	require.NoError(t, err)

	wrapped, err := hostkey.WithCheckHostIP(inner, khFile, true)
	require.NoError(t, err)

	addr, err := net.ResolveTCPAddr("tcp", "192.0.2.1:22")
	require.NoError(t, err)

	require.NoError(t, wrapped("example.com:22", addr, legit.PublicKey()), "permissive mode must not return error on IP mismatch")
}
