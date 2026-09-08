package hostkey_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

// denyRead puts mode on path -- one that leaves this process unable to read the
// file -- and restores it when the test ends.
//
// Where a mode cannot deny a read the test is skipped rather than asserting
// something the platform will not do: Windows honours only the read-only
// attribute and lets every read through whatever the bits say, and root ignores
// them as well.
func denyRead(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("file mode bits do not deny reads on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root reads a file whatever its mode says")
	}
	require.NoError(t, os.Chmod(path, mode))
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
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

func TestWithAliasStoresEntryUnderAlias(t *testing.T) {
	signer := newTestSigner(t)
	khFile := writeKnownHostsFile(t) // empty

	base, err := hostkey.KnownHostsFileCallback(khFile, false, false)
	require.NoError(t, err)

	wrapped := hostkey.WithAlias(base, "my-alias")

	addr, err := net.ResolveTCPAddr("tcp", "10.0.0.5:22")
	require.NoError(t, err)

	// First call: unknown host → appended under alias, not the IP.
	require.NoError(t, wrapped("10.0.0.5:22", addr, signer.PublicKey()))

	contents, err := os.ReadFile(khFile)
	require.NoError(t, err)
	require.Contains(t, string(contents), "my-alias", "entry must be stored under the alias")
	require.NotContains(t, string(contents), "10.0.0.5", "real IP must not appear in known_hosts")

	// Second call: now known under alias → accepted without error.
	require.NoError(t, wrapped("10.0.0.5:22", addr, signer.PublicKey()), "aliased host must be accepted on second connection")
}

func TestWithAliasLookupByAlias(t *testing.T) {
	signer := newTestSigner(t)

	// Pre-populate known_hosts with the alias, not the IP.
	line := knownhosts.Line([]string{knownhosts.Normalize("my-alias:22")}, signer.PublicKey())
	khFile := writeKnownHostsFile(t, line)

	base, err := hostkey.KnownHostsFileCallback(khFile, false, false)
	require.NoError(t, err)

	wrapped := hostkey.WithAlias(base, "my-alias")

	addr, err := net.ResolveTCPAddr("tcp", "10.0.0.5:22")
	require.NoError(t, err)

	require.NoError(t, wrapped("10.0.0.5:22", addr, signer.PublicKey()), "pre-existing alias entry must match")
}

func TestWithAliasRejectsInvalidAliases(t *testing.T) {
	signer := newTestSigner(t)
	khFile := writeKnownHostsFile(t)

	base, err := hostkey.KnownHostsFileCallback(khFile, false, false)
	require.NoError(t, err)

	addr, err := net.ResolveTCPAddr("tcp", "10.0.0.5:22")
	require.NoError(t, err)

	invalid := []string{
		"",                 // empty — produces malformed host pattern
		"my alias",         // space
		"tab\there",        // tab
		"newline\nhere",    // newline
		"cr\rhere",         // carriage return
		"host1,host2",      // comma — would inject multiple patterns
		"example.com:2222", // alias already contains a port
		"[::1]:22",         // bracketed IPv6 with port
	}
	for _, alias := range invalid {
		wrapped := hostkey.WithAlias(base, alias)
		err := wrapped("10.0.0.5:22", addr, signer.PublicKey())
		require.ErrorIs(t, err, hostkey.ErrHostKeyMismatch, "alias %q must be rejected", alias)
	}
}

func TestWithAliasBracketedIPv6(t *testing.T) {
	signer := newTestSigner(t)

	// Pre-populate known_hosts using the bracketed host+port form that knownhosts.Normalize produces.
	line := knownhosts.Line([]string{knownhosts.Normalize("[2001:db8::1]:22")}, signer.PublicKey())
	khFile := writeKnownHostsFile(t, line)

	base, err := hostkey.KnownHostsFileCallback(khFile, false, false)
	require.NoError(t, err)

	// Caller passes the bracketed form as the alias (as ssh_config may produce).
	wrapped := hostkey.WithAlias(base, "[2001:db8::1]")

	addr, err := net.ResolveTCPAddr("tcp", "10.0.0.5:22")
	require.NoError(t, err)

	require.NoError(t, wrapped("10.0.0.5:22", addr, signer.PublicKey()),
		"bracketed IPv6 alias must be unbracketed before JoinHostPort and match the known_hosts entry")
}

func TestWithAliasNilRemoteDoesNotPanic(t *testing.T) {
	signer := newTestSigner(t)
	khFile := writeKnownHostsFile(t)

	base, err := hostkey.KnownHostsFileCallback(khFile, false, false)
	require.NoError(t, err)

	wrapped := hostkey.WithAlias(base, "my-alias")

	// nil remote must not panic; the new entry is appended under the alias.
	require.NotPanics(t, func() {
		_ = wrapped("10.0.0.5:22", nil, signer.PublicKey())
	})
}

func TestWithAliasMismatchedKeyReturnsError(t *testing.T) {
	right := newTestSigner(t)
	wrong := newTestSigner(t)

	line := knownhosts.Line([]string{knownhosts.Normalize("my-alias:22")}, right.PublicKey())
	khFile := writeKnownHostsFile(t, line)

	base, err := hostkey.KnownHostsFileCallback(khFile, false, false)
	require.NoError(t, err)

	wrapped := hostkey.WithAlias(base, "my-alias")

	addr, err := net.ResolveTCPAddr("tcp", "10.0.0.5:22")
	require.NoError(t, err)

	err = wrapped("10.0.0.5:22", addr, wrong.PublicKey())
	require.ErrorIs(t, err, hostkey.ErrHostKeyMismatch)
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

// A writePath of /dev/null keeps no record of a new key, but the files beside
// it are still the trust set: a key one of them contradicts has to stay a
// mismatch, or an empty write target would overrule a system-wide file.
func TestKnownHostsFilesCallbackDevNullConsultsAlsoVerify(t *testing.T) {
	trusted := newTestSigner(t)
	presented := newTestSigner(t)

	global := writeKnownHostsFile(t,
		knownhosts.Line([]string{knownhosts.Normalize("192.0.2.1:22")}, trusted.PublicKey()),
	)

	addr, err := net.ResolveTCPAddr("tcp", "192.0.2.1:22")
	require.NoError(t, err)

	cb, err := hostkey.KnownHostsFilesCallback("/dev/null", []string{global}, false, false)
	require.NoError(t, err)
	require.ErrorIs(t, cb("192.0.2.1:22", addr, presented.PublicKey()), hostkey.ErrHostKeyMismatch,
		"a key the read-only trust set contradicts must be refused even when nothing can be recorded")

	require.NoError(t, cb("192.0.2.1:22", addr, trusted.PublicKey()),
		"the key the trust set holds must still be accepted")
}

func TestKnownHostsFilesCallbackDevNullAcceptsUnknownHostWithoutRecording(t *testing.T) {
	trusted := newTestSigner(t)
	unknown := newTestSigner(t)

	global := writeKnownHostsFile(t,
		knownhosts.Line([]string{knownhosts.Normalize("192.0.2.1:22")}, trusted.PublicKey()),
	)
	before, err := os.ReadFile(global)
	require.NoError(t, err)

	addr, err := net.ResolveTCPAddr("tcp", "198.51.100.7:22")
	require.NoError(t, err)

	cb, err := hostkey.KnownHostsFilesCallback("/dev/null", []string{global}, false, false)
	require.NoError(t, err)
	require.NoError(t, cb("198.51.100.7:22", addr, unknown.PublicKey()),
		"a host no file knows is accept-new, and /dev/null is where the record goes")

	after, err := os.ReadFile(global)
	require.NoError(t, err)
	require.Equal(t, string(before), string(after),
		"a read-only trust source must never be appended to")
}

func TestKnownHostsFilesCallbackDevNullWithNoOtherSourceVerifiesNothing(t *testing.T) {
	addr, err := net.ResolveTCPAddr("tcp", "192.0.2.1:22")
	require.NoError(t, err)

	cb, err := hostkey.KnownHostsFilesCallback("/dev/null", nil, false, false)
	require.NoError(t, err)
	require.NoError(t, cb("192.0.2.1:22", addr, newTestSigner(t).PublicKey()),
		"with no trust set left, /dev/null keeps its usual meaning of not verifying host keys")
}

func TestKnownHostsFilesCallbackDevNullPermissiveToleratesMismatch(t *testing.T) {
	trusted := newTestSigner(t)
	presented := newTestSigner(t)

	global := writeKnownHostsFile(t,
		knownhosts.Line([]string{knownhosts.Normalize("192.0.2.1:22")}, trusted.PublicKey()),
	)

	addr, err := net.ResolveTCPAddr("tcp", "192.0.2.1:22")
	require.NoError(t, err)

	cb, err := hostkey.KnownHostsFilesCallback("/dev/null", []string{global}, true, false)
	require.NoError(t, err)
	require.NoError(t, cb("192.0.2.1:22", addr, presented.PublicKey()),
		"permissive downgrades a mismatch to a warning on this path too")
}

func TestKnownHostsFilesCallbackWithIPCheckDevNullConsultsAlsoVerify(t *testing.T) {
	legit := newTestSigner(t)
	spoof := newTestSigner(t)

	global := writeKnownHostsFile(t,
		knownhosts.Line([]string{knownhosts.Normalize("example.com:22")}, legit.PublicKey()),
		knownhosts.Line([]string{knownhosts.Normalize("192.0.2.1:22")}, spoof.PublicKey()),
	)
	stubLookupHost(t, func(_ string) ([]string, error) {
		return []string{"192.0.2.1"}, nil
	})

	addr, err := net.ResolveTCPAddr("tcp", "192.0.2.1:22")
	require.NoError(t, err)

	cb, err := hostkey.KnownHostsFilesCallbackWithIPCheck("/dev/null", []string{global}, false, false)
	require.NoError(t, err)

	cbErr := cb("example.com:22", addr, legit.PublicKey())
	require.ErrorIs(t, cbErr, hostkey.ErrHostKeyMismatch,
		"the IP check has to run against the read-only trust set as well")
	require.Contains(t, cbErr.Error(), "192.0.2.1")
}

// A callback outlives the connection it was built for, and knownhosts.New reads
// the files only once. Recording a key therefore has to reach the checker too,
// or a second connection through the same callback would still find the host
// unknown and accept whatever key it was offered -- the very change accept-new
// exists to refuse.
func TestKnownHostsFileCallbackReuseRefusesChangedKey(t *testing.T) {
	for _, hash := range []bool{false, true} {
		t.Run(fmt.Sprintf("hash=%v", hash), func(t *testing.T) {
			khFile := filepath.Join(t.TempDir(), "known_hosts")
			cb, err := hostkey.KnownHostsFileCallback(khFile, false, hash)
			require.NoError(t, err)

			addr, err := net.ResolveTCPAddr("tcp", "192.0.2.1:22")
			require.NoError(t, err)

			first := newTestSigner(t)
			require.NoError(t, cb("192.0.2.1:22", addr, first.PublicKey()),
				"an unknown host is recorded and accepted")

			recorded, err := os.ReadFile(khFile)
			require.NoError(t, err)
			require.Equal(t, 1, strings.Count(string(recorded), "\n"), "exactly one entry is recorded")

			second := newTestSigner(t)
			cbErr := cb("192.0.2.1:22", addr, second.PublicKey())
			require.ErrorIs(t, cbErr, hostkey.ErrHostKeyMismatch,
				"the key recorded a moment ago must be honoured by the same callback")

			after, err := os.ReadFile(khFile)
			require.NoError(t, err)
			require.Equal(t, string(recorded), string(after), "a refused key must not be recorded")
		})
	}
}

// Two connections can build their callbacks before either handshakes, leaving
// each with its own snapshot of the same file. A key one of them records has to
// count for the other, or both would find the host new and record a key of its
// own for it.
func TestKnownHostsFileCallbackSeesKeyRecordedByAnotherCallback(t *testing.T) {
	khFile := filepath.Join(t.TempDir(), "known_hosts")
	first, err := hostkey.KnownHostsFileCallback(khFile, false, false)
	require.NoError(t, err)
	second, err := hostkey.KnownHostsFileCallback(khFile, false, false)
	require.NoError(t, err)

	addr, err := net.ResolveTCPAddr("tcp", "192.0.2.1:22")
	require.NoError(t, err)

	recorded := newTestSigner(t)
	require.NoError(t, first("192.0.2.1:22", addr, recorded.PublicKey()))

	changed := newTestSigner(t)
	require.ErrorIs(t, second("192.0.2.1:22", addr, changed.PublicKey()), hostkey.ErrHostKeyMismatch,
		"a key recorded by one callback must be honoured by the other")
	require.NoError(t, second("192.0.2.1:22", addr, recorded.PublicKey()),
		"and the recorded key itself is simply known")

	contents, err := os.ReadFile(khFile)
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(contents), "\n"), "the second callback must not record a key of its own")
}

// The same reasoning covers a file another process appended to: what the
// callback verifies against is the file as it is now, not as it was when the
// connection was set up.
func TestKnownHostsFileCallbackSeesExternallyRecordedKey(t *testing.T) {
	khFile := filepath.Join(t.TempDir(), "known_hosts")
	cb, err := hostkey.KnownHostsFileCallback(khFile, false, false)
	require.NoError(t, err)

	recorded := newTestSigner(t)
	line := knownhosts.Line([]string{knownhosts.Normalize("192.0.2.1:22")}, recorded.PublicKey())
	require.NoError(t, os.WriteFile(khFile, []byte(line+"\n"), 0o600))

	addr, err := net.ResolveTCPAddr("tcp", "192.0.2.1:22")
	require.NoError(t, err)

	require.NoError(t, cb("192.0.2.1:22", addr, recorded.PublicKey()))

	contents, err := os.ReadFile(khFile)
	require.NoError(t, err)
	require.Equal(t, line+"\n", string(contents), "an entry already on file must be recognised, not recorded again")

	require.ErrorIs(t, cb("192.0.2.1:22", addr, newTestSigner(t).PublicKey()), hostkey.ErrHostKeyMismatch)
}

// A key replaced by one of the same algorithm produces a line of exactly the
// same length, and a copy that preserves timestamps puts the modification time
// back. Neither size nor mtime is therefore evidence that the trust set is
// unchanged, so the files are read rather than cached.
func TestKnownHostsFileCallbackSeesSameSizeSameMtimeRewrite(t *testing.T) {
	khFile := filepath.Join(t.TempDir(), "known_hosts")

	before := newTestSigner(t)
	oldLine := knownhosts.Line([]string{knownhosts.Normalize("192.0.2.1:22")}, before.PublicKey()) + "\n"
	require.NoError(t, os.WriteFile(khFile, []byte(oldLine), 0o600))

	stat, err := os.Stat(khFile)
	require.NoError(t, err)
	mtime := stat.ModTime()

	cb, err := hostkey.KnownHostsFileCallback(khFile, false, false)
	require.NoError(t, err)

	addr, err := net.ResolveTCPAddr("tcp", "192.0.2.1:22")
	require.NoError(t, err)
	require.NoError(t, cb("192.0.2.1:22", addr, before.PublicKey()))

	after := newTestSigner(t)
	newLine := knownhosts.Line([]string{knownhosts.Normalize("192.0.2.1:22")}, after.PublicKey()) + "\n"
	require.Len(t, newLine, len(oldLine), "same algorithm, same line length — the rewrite has to be invisible to a size check")
	require.NoError(t, os.WriteFile(khFile, []byte(newLine), 0o600))
	require.NoError(t, os.Chtimes(khFile, mtime, mtime), "and invisible to an mtime check too")

	require.ErrorIs(t, cb("192.0.2.1:22", addr, before.PublicKey()), hostkey.ErrHostKeyMismatch,
		"the replaced key must be refused")
	require.NoError(t, cb("192.0.2.1:22", addr, after.PublicKey()), "and the key now on file accepted")
}

// The IP check runs before the hostname check, so it needs the files as they are
// now just as much: an entry for the IP added after the callback was built is
// what catches a spoofed key.
func TestCheckHostIPSeesEntryAddedAfterConstruction(t *testing.T) {
	legit := newTestSigner(t)
	spoof := newTestSigner(t)

	khFile := writeKnownHostsFile(t,
		knownhosts.Line([]string{knownhosts.Normalize("example.com:22")}, legit.PublicKey()),
	)
	stubLookupHost(t, func(_ string) ([]string, error) {
		return []string{"192.0.2.1"}, nil
	})

	cb, err := hostkey.KnownHostsFileCallbackWithIPCheck(khFile, false, false)
	require.NoError(t, err)

	contents, err := os.ReadFile(khFile)
	require.NoError(t, err)
	spoofed := knownhosts.Line([]string{knownhosts.Normalize("192.0.2.1:22")}, spoof.PublicKey())
	require.NoError(t, os.WriteFile(khFile, append(contents, []byte(spoofed+"\n")...), 0o600))

	addr, err := net.ResolveTCPAddr("tcp", "192.0.2.1:22")
	require.NoError(t, err)

	cbErr := cb("example.com:22", addr, legit.PublicKey())
	require.ErrorIs(t, cbErr, hostkey.ErrHostKeyMismatch, "an IP entry added since must still be consulted")
	require.Contains(t, cbErr.Error(), "192.0.2.1")
}

// A trust file that goes missing takes its entries with it, but must not take
// the connection: the other sources still answer, and the file counts again as
// soon as it is back.
func TestKnownHostsFilesCallbackToleratesVanishedVerifyPath(t *testing.T) {
	recorded := newTestSigner(t)
	global := writeKnownHostsFile(t,
		knownhosts.Line([]string{knownhosts.Normalize("192.0.2.1:22")}, recorded.PublicKey()),
	)
	khFile := filepath.Join(t.TempDir(), "known_hosts")

	cb, err := hostkey.KnownHostsFilesCallback(khFile, []string{global}, false, false)
	require.NoError(t, err)

	addr, err := net.ResolveTCPAddr("tcp", "192.0.2.1:22")
	require.NoError(t, err)
	require.ErrorIs(t, cb("192.0.2.1:22", addr, newTestSigner(t).PublicKey()), hostkey.ErrHostKeyMismatch,
		"the global file contradicts this key")

	require.NoError(t, os.Remove(global))
	require.NoError(t, cb("192.0.2.1:22", addr, recorded.PublicKey()),
		"with the file gone the host is simply unknown, and accept-new records it")
}

// A file that stats fine but cannot be opened is the case a stat-based filter
// misses: knownhosts.New fails on it, and one unreadable file must not take the
// rest of the trust set down with it.
func TestKnownHostsFilesCallbackToleratesUnreadableVerifyPath(t *testing.T) {
	recorded := newTestSigner(t)
	global := writeKnownHostsFile(t,
		knownhosts.Line([]string{knownhosts.Normalize("192.0.2.1:22")}, recorded.PublicKey()),
	)
	unreadable := writeKnownHostsFile(t)
	khFile := filepath.Join(t.TempDir(), "known_hosts")

	cb, err := hostkey.KnownHostsFilesCallback(khFile, []string{global, unreadable}, false, false)
	require.NoError(t, err)

	denyRead(t, unreadable, 0o000)

	addr, err := net.ResolveTCPAddr("tcp", "192.0.2.1:22")
	require.NoError(t, err)
	require.ErrorIs(t, cb("192.0.2.1:22", addr, newTestSigner(t).PublicKey()), hostkey.ErrHostKeyMismatch,
		"the readable file still contradicts this key")
	require.NoError(t, cb("192.0.2.1:22", addr, recorded.PublicKey()),
		"and still vouches for the recorded one")
}

// The file a key is recorded in is not an optional source: the keys it holds
// are what tell a host that has been seen before from a new one. Treating an
// unreadable one as empty would make every host look new, and accept-new would
// append and accept a key the file already contradicts.
func TestKnownHostsFileCallbackRefusesUnreadableWriteTarget(t *testing.T) {
	recorded := newTestSigner(t)
	khFile := writeKnownHostsFile(t,
		knownhosts.Line([]string{knownhosts.Normalize("192.0.2.1:22")}, recorded.PublicKey()),
	)

	cb, err := hostkey.KnownHostsFileCallback(khFile, false, false)
	require.NoError(t, err)

	// Appendable but no longer readable, the shape a write-only known_hosts has.
	denyRead(t, khFile, 0o200)

	addr, err := net.ResolveTCPAddr("tcp", "192.0.2.1:22")
	require.NoError(t, err)

	err = cb("192.0.2.1:22", addr, newTestSigner(t).PublicKey())
	require.Error(t, err, "a key must not be accepted against a trust source that could not be read")
	require.ErrorIs(t, err, hostkey.ErrCheckHostKey)

	require.NoError(t, os.Chmod(khFile, 0o600))
	contents, err := os.ReadFile(khFile)
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(contents), "\n"), "and nothing must have been appended")
}

// A write target that is already unreadable is refused when the callback is
// built, rather than after it has accepted something.
func TestKnownHostsFileCallbackRejectsUnreadableWriteTargetUpFront(t *testing.T) {
	khFile := writeKnownHostsFile(t)
	denyRead(t, khFile, 0o200)

	_, err := hostkey.KnownHostsFileCallback(khFile, false, false)
	require.ErrorIs(t, err, hostkey.ErrCheckHostKey)
}

// An unreadable file named as the trust source at construction is a different
// matter: there is nothing left to verify against, so it is reported.
func TestKnownHostsCallbacksRejectUnreadableTrustSource(t *testing.T) {
	unreadable := writeKnownHostsFile(t)
	denyRead(t, unreadable, 0o000)

	_, err := hostkey.KnownHostsReadOnlyFileCallback(unreadable, false)
	require.ErrorIs(t, err, hostkey.ErrCheckHostKey)

	_, err = hostkey.KnownHostsFilesCallback(filepath.Join(t.TempDir(), "known_hosts"), []string{unreadable}, false, false)
	require.ErrorIs(t, err, hostkey.ErrCheckHostKey)
}

// WithCheckHostIP takes the trust source for the IP check as its own argument,
// so an unusable one has to be an error: silently checking nothing would leave
// the caller believing the IP was verified.
func TestWithCheckHostIPRejectsUnusablePath(t *testing.T) {
	base, err := hostkey.KnownHostsReadOnlyFileCallback(writeKnownHostsFile(t), false)
	require.NoError(t, err)

	_, err = hostkey.WithCheckHostIP(base, filepath.Join(t.TempDir(), "known_hosts"), false)
	require.ErrorIs(t, err, hostkey.ErrCheckHostKey, "a missing path must be reported")

	_, err = hostkey.WithCheckHostIP(base, t.TempDir(), false)
	require.ErrorIs(t, err, hostkey.ErrCheckHostKey, "and so must a directory")
}

// The trust set is what the constructor was given and checked. A caller that
// reuses its slice afterwards -- the same backing array for the next
// connection, say -- must not be able to move the sources out from under a
// callback that was already built.
func TestKnownHostsCallbacksKeepTheirOwnTrustSet(t *testing.T) {
	recorded := newTestSigner(t)
	real := writeKnownHostsFile(t,
		knownhosts.Line([]string{knownhosts.Normalize("192.0.2.1:22")}, recorded.PublicKey()),
	)
	empty := writeKnownHostsFile(t)

	addr, err := net.ResolveTCPAddr("tcp", "192.0.2.1:22")
	require.NoError(t, err)

	paths := []string{real}
	readOnly, err := hostkey.KnownHostsReadOnlyFilesCallback(paths, false)
	require.NoError(t, err)

	khFile := filepath.Join(t.TempDir(), "known_hosts")
	recording, err := hostkey.KnownHostsFilesCallback(khFile, paths, false, false)
	require.NoError(t, err)

	paths[0] = empty // the caller reuses its slice for something else

	require.NoError(t, readOnly("192.0.2.1:22", addr, recorded.PublicKey()),
		"the read-only callback still verifies against the file it was built with")
	require.ErrorIs(t, recording("192.0.2.1:22", addr, newTestSigner(t).PublicKey()), hostkey.ErrHostKeyMismatch,
		"and the recording one still sees the key that file holds, rather than an empty trust set")
}

// A trust source the caller named but that cannot be read is a mistake worth
// reporting rather than a source to quietly do without -- on the recording
// paths just as on the /dev/null one.
func TestKnownHostsFilesCallbackRejectsUnusableVerifyPath(t *testing.T) {
	khFile := filepath.Join(t.TempDir(), "known_hosts")
	missing := filepath.Join(t.TempDir(), "ssh_known_hosts")

	_, err := hostkey.KnownHostsFilesCallback(khFile, []string{missing}, false, false)
	require.ErrorIs(t, err, hostkey.ErrCheckHostKey, "a verify path that does not exist must be reported")
	require.NoFileExists(t, khFile, "and the append target must not be created on the way to that error")

	_, err = hostkey.KnownHostsFilesCallbackWithIPCheck(khFile, []string{missing}, false, false)
	require.ErrorIs(t, err, hostkey.ErrCheckHostKey, "the IP-checking variant takes the same argument, and must hold it to the same rule")

	_, err = hostkey.KnownHostsFilesCallback(khFile, []string{t.TempDir()}, false, false)
	require.ErrorIs(t, err, hostkey.ErrCheckHostKey, "a directory holds no entries either")
}

// The same key offered twice is known the second time, so there is nothing left
// to record: a duplicate line would be harmless but would grow the file on every
// connection.
func TestKnownHostsFileCallbackReuseRecordsOnce(t *testing.T) {
	khFile := filepath.Join(t.TempDir(), "known_hosts")
	cb, err := hostkey.KnownHostsFileCallback(khFile, false, false)
	require.NoError(t, err)

	addr, err := net.ResolveTCPAddr("tcp", "192.0.2.1:22")
	require.NoError(t, err)

	signer := newTestSigner(t)
	require.NoError(t, cb("192.0.2.1:22", addr, signer.PublicKey()))
	require.NoError(t, cb("192.0.2.1:22", addr, signer.PublicKey()))

	contents, err := os.ReadFile(khFile)
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(contents), "\n"), "the key is recorded once, not once per connection")
}

func TestKnownHostsFilesCallbackDevNullRejectsUnusableAlsoVerify(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "ssh_known_hosts")

	_, err := hostkey.KnownHostsFilesCallback("/dev/null", []string{missing}, false, false)
	require.ErrorIs(t, err, hostkey.ErrCheckHostKey,
		"a trust source that cannot be read must be reported, not silently contribute nothing")
}
