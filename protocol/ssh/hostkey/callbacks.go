// Package hostkey implements a callback for the ssh.ClientConfig.HostKeyCallback
package hostkey

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// lookupHost resolves a hostname to IP addresses. Overridable in tests.
// Access must be serialized with lookupHostMu.
var (
	lookupHostMu sync.Mutex
	lookupHost   = net.LookupHost
)

const devNull = "/dev/null"

var (
	// ErrHostKeyMismatch is returned when the host key does not match the host key or a key in known_hosts file.
	ErrHostKeyMismatch = errors.New("host key mismatch")

	// ErrCheckHostKey is returned when the callback could not be created.
	ErrCheckHostKey = errors.New("check hostkey")

	// InsecureIgnoreHostKeyCallback is an insecure HostKeyCallback that accepts any host key.
	InsecureIgnoreHostKeyCallback = ssh.InsecureIgnoreHostKey() //nolint:gosec

	mu sync.Mutex
)

// StaticKeyCallback returns a HostKeyCallback that checks the host key against a given host key.
func StaticKeyCallback(trustedKey string) ssh.HostKeyCallback {
	return func(_ string, _ net.Addr, k ssh.PublicKey) error {
		ks := keyString(k)
		if trustedKey != ks {
			return ErrHostKeyMismatch
		}

		return nil
	}
}

// KnownHostsPathFromEnv returns the path to a known_hosts file from the environment variable SSH_KNOWN_HOSTS.
var KnownHostsPathFromEnv = func() (string, bool) {
	return os.LookupEnv("SSH_KNOWN_HOSTS")
}

// KnownHostsFileCallback returns a HostKeyCallback that uses a known hosts file to verify host keys.
func KnownHostsFileCallback(path string, permissive, hash bool) (ssh.HostKeyCallback, error) {
	if path == devNull {
		return InsecureIgnoreHostKeyCallback, nil
	}

	mu.Lock()
	defer mu.Unlock()

	if err := ensureFile(path); err != nil {
		return nil, err
	}

	hkc, err := knownhosts.New(path)
	if err != nil {
		return nil, fmt.Errorf("%w: knownhosts callback: %w", ErrCheckHostKey, err)
	}

	return wrapCallback(hkc, path, permissive, hash), nil
}

// KnownHostsReadOnlyFileCallback returns a HostKeyCallback that only reads from
// an existing known hosts file — it never creates the file or appends new entries.
// This is appropriate for system-wide files such as /etc/ssh/ssh_known_hosts that
// should not be modified by unprivileged users.
func KnownHostsReadOnlyFileCallback(path string, permissive bool) (ssh.HostKeyCallback, error) {
	if path == devNull {
		return InsecureIgnoreHostKeyCallback, nil
	}

	stat, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCheckHostKey, err)
	}
	if !stat.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %s is not a regular file", ErrCheckHostKey, path)
	}

	mu.Lock()
	defer mu.Unlock()

	hkc, err := knownhosts.New(path)
	if err != nil {
		return nil, fmt.Errorf("%w: knownhosts callback: %w", ErrCheckHostKey, err)
	}

	return wrapReadOnlyCallback(hkc, permissive), nil
}

// KnownHostsFileCallbackWithIPCheck is like KnownHostsFileCallback but also
// verifies the connecting IP address. It parses the known_hosts file once,
// sharing the checker between hostname and IP verification.
func KnownHostsFileCallbackWithIPCheck(path string, permissive, hash bool) (ssh.HostKeyCallback, error) {
	if path == devNull {
		return InsecureIgnoreHostKeyCallback, nil
	}

	mu.Lock()
	defer mu.Unlock()

	if err := ensureFile(path); err != nil {
		return nil, err
	}

	hkc, err := knownhosts.New(path)
	if err != nil {
		return nil, fmt.Errorf("%w: knownhosts callback: %w", ErrCheckHostKey, err)
	}

	return wrapCheckHostIP(wrapCallback(hkc, path, permissive, hash), hkc, permissive), nil
}

// KnownHostsReadOnlyFileCallbackWithIPCheck is like KnownHostsReadOnlyFileCallback
// but also verifies the connecting IP address. It parses the known_hosts file once,
// sharing the checker between hostname and IP verification.
func KnownHostsReadOnlyFileCallbackWithIPCheck(path string, permissive bool) (ssh.HostKeyCallback, error) {
	if path == devNull {
		return InsecureIgnoreHostKeyCallback, nil
	}

	stat, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCheckHostKey, err)
	}
	if !stat.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %s is not a regular file", ErrCheckHostKey, path)
	}

	mu.Lock()
	defer mu.Unlock()

	hkc, err := knownhosts.New(path)
	if err != nil {
		return nil, fmt.Errorf("%w: knownhosts callback: %w", ErrCheckHostKey, err)
	}

	return wrapCheckHostIP(wrapReadOnlyCallback(hkc, permissive), hkc, permissive), nil
}

// extends a knownhosts callback to not return an error when the key
// is not found in the known_hosts file but instead adds it to the file as new
// entry.
func wrapCallback(hkc ssh.HostKeyCallback, path string, permissive, hash bool) ssh.HostKeyCallback {
	// TODO this should also support the "accept-new" of StrictHostKeyChecking and possibly some other options.
	return ssh.HostKeyCallback(func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		mu.Lock()
		defer mu.Unlock()
		err := hkc(hostname, remote, key)
		if err == nil {
			return nil
		}

		var keyErr *knownhosts.KeyError
		if !errors.As(err, &keyErr) || len(keyErr.Want) > 0 {
			// keyErr.Want is empty if the host key is not in the known_hosts file
			// non-empty is a mismatch
			if permissive {
				fmt.Fprintln(os.Stderr, "Ignored an SSH host key mismatch for", remote, "because StrictHostKeyChecking is set to 'no' in ssh config")
				return nil
			}
			return fmt.Errorf("%w: %w", ErrHostKeyMismatch, err)
		}

		dbFile, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return fmt.Errorf("failed to open ssh known_hosts file %s for writing: %w", path, err)
		}

		knownHostsEntry := knownhosts.Normalize(remote.String())
		if hash {
			knownHostsEntry = knownhosts.HashHostname(knownHostsEntry)
		}

		row := knownhosts.Line([]string{knownHostsEntry}, key)
		row = strings.TrimSpace(row) + "\n"

		if _, err := dbFile.WriteString(row); err != nil {
			return fmt.Errorf("failed to write to known hosts file %s: %w", path, err)
		}
		if err := dbFile.Close(); err != nil {
			return fmt.Errorf("failed to close known_hosts file after writing: %w", err)
		}
		return nil
	})
}

// wrapReadOnlyCallback wraps a knownhosts callback to reject unknown hosts
// without writing to the file. When permissive is true, unknown hosts are
// accepted silently instead of rejected.
func wrapReadOnlyCallback(hkc ssh.HostKeyCallback, permissive bool) ssh.HostKeyCallback {
	return ssh.HostKeyCallback(func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		mu.Lock()
		defer mu.Unlock()
		err := hkc(hostname, remote, key)
		if err == nil {
			return nil
		}

		var keyErr *knownhosts.KeyError
		if !errors.As(err, &keyErr) {
			// unexpected error unrelated to key matching (e.g. address parsing, IO)
			if permissive {
				fmt.Fprintln(os.Stderr, "Ignored an SSH host key error for", remote, "because StrictHostKeyChecking is set to 'no' in ssh config")
				return nil
			}
			return fmt.Errorf("%w: %w", ErrHostKeyMismatch, err)
		}
		if len(keyErr.Want) > 0 {
			// non-empty Want means a known host presented a different key
			if permissive {
				fmt.Fprintln(os.Stderr, "Ignored an SSH host key mismatch for", remote, "because StrictHostKeyChecking is set to 'no' in ssh config")
				return nil
			}
			return fmt.Errorf("%w: %w", ErrHostKeyMismatch, err)
		}

		// host not found in file — read-only mode cannot append new entries
		if permissive {
			return nil
		}
		return fmt.Errorf("%w: unknown host: %w", ErrHostKeyMismatch, err)
	})
}

// WithCheckHostIP wraps cb to also verify the connecting IP address in
// known_hosts. When the remote address is a TCP connection the actual connected
// IP is checked directly; otherwise all DNS-resolved addresses are checked.
// If the IP is found in known_hosts with a different key (potential DNS
// spoofing), ErrHostKeyMismatch is returned. DNS resolution failures are
// non-fatal. Skipped when hostname is already an IP address. Unlike OpenSSH,
// this implementation never writes IP addresses to known_hosts.
func WithCheckHostIP(cb ssh.HostKeyCallback, path string, permissive bool) (ssh.HostKeyCallback, error) {
	if path == devNull {
		return cb, nil
	}
	mu.Lock()
	rawChecker, err := knownhosts.New(path)
	mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("%w: check-host-ip: %w", ErrCheckHostKey, err)
	}
	return wrapCheckHostIP(cb, rawChecker, permissive), nil
}

// checkResolvedIP checks a single resolved IP address against rawChecker.
// Returns an error for a known key mismatch (potential DNS spoofing) and, in
// strict mode, also for unexpected checker errors (IO, address parsing).
// Must be called with mu held.
func checkResolvedIP(rawChecker ssh.HostKeyCallback, addr, port string, remote net.Addr, key ssh.PublicKey, permissive bool) error {
	effectivePort := port
	ipAddr := &net.TCPAddr{IP: net.ParseIP(addr)}
	if tcp, ok := remote.(*net.TCPAddr); ok {
		ipAddr.Port = tcp.Port
		effectivePort = strconv.Itoa(tcp.Port)
	} else if p, err := strconv.Atoi(port); err == nil {
		ipAddr.Port = p
	}
	ipHost := net.JoinHostPort(addr, effectivePort)
	ipErr := rawChecker(ipHost, ipAddr, key)
	if ipErr == nil {
		return nil
	}
	var keyErr *knownhosts.KeyError
	if !errors.As(ipErr, &keyErr) {
		// unexpected error (e.g. IO, address parsing) — surface in strict mode
		if permissive {
			fmt.Fprintln(os.Stderr, "Ignored SSH host key check error for resolved IP", addr, "because StrictHostKeyChecking is set to 'no' in ssh config")
			return nil
		}
		return fmt.Errorf("%w: resolved IP %s: %w", ErrHostKeyMismatch, addr, ipErr)
	}
	if len(keyErr.Want) == 0 {
		return nil // unknown IP in detection-only mode is not an error
	}
	if permissive {
		fmt.Fprintln(os.Stderr, "Ignored SSH host key mismatch for resolved IP", addr, "because StrictHostKeyChecking is set to 'no' in ssh config")
		return nil
	}
	return fmt.Errorf("%w: resolved IP %s: %w", ErrHostKeyMismatch, addr, ipErr)
}

func wrapCheckHostIP(callback, rawChecker ssh.HostKeyCallback, permissive bool) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		host, port, err := net.SplitHostPort(hostname)
		if err != nil {
			// hostname has no port — use it as-is and derive the port from the remote address
			host = hostname
			if tcp, ok := remote.(*net.TCPAddr); ok {
				port = strconv.Itoa(tcp.Port)
			}
		}

		// Perform the IP check before invoking callback so that a spoofed key is
		// never written to known_hosts when a mismatch is detected.
		if net.ParseIP(host) == nil && port != "" {
			if ipErr := checkHostIP(rawChecker, host, port, remote, key, permissive); ipErr != nil {
				return ipErr
			}
		}

		return callback(hostname, remote, key)
	}
}

// checkHostIP verifies the connecting IP address in known_hosts. When the
// remote is a TCP connection the actual connected IP is checked directly;
// otherwise all DNS-resolved addresses are checked. DNS resolution failures
// are non-fatal.
func checkHostIP(rawChecker ssh.HostKeyCallback, host, port string, remote net.Addr, key ssh.PublicKey, permissive bool) error {
	// When the connected address is known, check only that IP to avoid
	// false positives from multi-homed hostnames (round-robin/CDN).
	if tcp, ok := remote.(*net.TCPAddr); ok && tcp.IP != nil {
		mu.Lock()
		err := checkResolvedIP(rawChecker, tcp.IP.String(), port, remote, key, permissive)
		mu.Unlock()
		return err
	}

	lookupHostMu.Lock()
	resolve := lookupHost
	lookupHostMu.Unlock()

	addrs, dnsErr := resolve(host)
	if dnsErr != nil {
		return nil //nolint:nilerr // DNS resolution failures are non-fatal per doc comment.
	}

	var loopErr error
	mu.Lock()
	for _, addr := range addrs {
		if loopErr = checkResolvedIP(rawChecker, addr, port, remote, key, permissive); loopErr != nil {
			break
		}
	}
	mu.Unlock()
	return loopErr
}

func fileExists(path string) bool {
	stat, err := os.Stat(path)
	return err == nil && stat.Mode().IsRegular()
}

func ensureDir(path string) error {
	stat, err := os.Stat(path)
	if err == nil && !stat.Mode().IsDir() {
		return fmt.Errorf("%w: path %s is not a directory", ErrCheckHostKey, path)
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", path, err)
	}
	return nil
}

func ensureFile(path string) error {
	if fileExists(path) {
		return nil
	}
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("failed to create known_hosts file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("failed to close known_hosts file: %w", err)
	}
	return nil
}

// aliasAddr wraps a net.Addr but returns alias (with the pre-derived port) from String.
// This lets the alias be used as the known_hosts entry instead of the real IP.
// The port is carried explicitly so that non-TCP remote addresses (whose String()
// may not include a port) produce a consistent entry.
type aliasAddr struct {
	alias string
	port  string
	orig  net.Addr
}

func (a aliasAddr) Network() string {
	if a.orig == nil {
		return "tcp"
	}
	return a.orig.Network()
}

func (a aliasAddr) String() string {
	if a.port != "" {
		return net.JoinHostPort(a.alias, a.port)
	}
	return a.alias
}

// WithAlias wraps callback so that alias replaces the actual hostname for all
// known_hosts lookups and new-entry storage. This implements the HostKeyAlias
// ssh_config option: connecting through a bastion or tunnel stores the entry
// under the logical alias, not the TCP address.
func WithAlias(callback ssh.HostKeyCallback, alias string) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		if alias == "" {
			return fmt.Errorf("%w: HostKeyAlias must not be empty", ErrHostKeyMismatch)
		}
		if strings.IndexFunc(alias, unicode.IsSpace) >= 0 {
			return fmt.Errorf("%w: HostKeyAlias %q contains whitespace", ErrHostKeyMismatch, alias)
		}
		if strings.ContainsRune(alias, ',') {
			return fmt.Errorf("%w: HostKeyAlias %q contains comma", ErrHostKeyMismatch, alias)
		}
		// Reject aliases that already embed a port — the port comes from the
		// connection, not the alias.
		if _, _, err := net.SplitHostPort(alias); err == nil {
			return fmt.Errorf("%w: HostKeyAlias %q must not include a port", ErrHostKeyMismatch, alias)
		}
		// Unbracket IPv6 literals so net.JoinHostPort re-brackets correctly
		// (e.g. "[2001:db8::1]" → "2001:db8::1" → "[2001:db8::1]:22").
		bareAlias := alias
		if len(alias) >= 2 && alias[0] == '[' && alias[len(alias)-1] == ']' {
			bareAlias = alias[1 : len(alias)-1]
		}
		_, port, err := net.SplitHostPort(hostname)
		if err != nil {
			// hostname has no port — try to derive it from remote
			port = ""
			if tcp, ok := remote.(*net.TCPAddr); ok && tcp.Port > 0 {
				port = strconv.Itoa(tcp.Port)
			}
		}
		var aliasHostname string
		if port != "" {
			aliasHostname = net.JoinHostPort(bareAlias, port)
		} else {
			aliasHostname = bareAlias
		}
		return callback(aliasHostname, aliasAddr{alias: bareAlias, port: port, orig: remote}, key)
	}
}

// create human-readable SSH-key strings e.g. "ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTY....".
func keyString(k ssh.PublicKey) string {
	return k.Type() + " " + base64.StdEncoding.EncodeToString(k.Marshal())
}
