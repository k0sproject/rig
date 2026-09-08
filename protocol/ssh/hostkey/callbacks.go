// Package hostkey implements a callback for the ssh.ClientConfig.HostKeyCallback
package hostkey

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
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
	return KnownHostsFilesCallback(path, nil, permissive, hash)
}

// KnownHostsFilesCallback verifies a host key against writePath and every file
// in alsoVerify, and records the key of a host that none of them knows in
// writePath.
//
// That split is OpenSSH's: a new entry is appended to the first file, while
// every file counts when deciding whether the host is new at all. Verifying
// against writePath alone would classify a host as new whenever the first file
// happens not to mention it -- and then record and accept a key that a later
// user file, or a system-wide one, says belongs to a different host key.
//
// Each alsoVerify path must be an existing regular file; writePath is created
// when it does not exist, since it is where a new key goes. That is a check on
// what the caller asked for: an alsoVerify file that goes missing later
// contributes nothing until it comes back, rather than failing every connection
// after it. writePath is held to more, before and after: it must be readable as
// well as writable, because a key it already holds is what tells a returning
// host from a new one, and a write-only file would make every host look new.
//
// A writePath of /dev/null keeps no record, so a host that alsoVerify does not
// know is accepted without being stored -- but alsoVerify is still consulted,
// and a key it contradicts is still a mismatch. Only when alsoVerify is empty
// as well does that leave nothing to verify against, which is taken as the
// usual meaning of the null device: no host key verification.
func KnownHostsFilesCallback(writePath string, alsoVerify []string, permissive, hash bool) (ssh.HostKeyCallback, error) {
	if writePath == devNull {
		return devNullTrustSet(alsoVerify, permissive, false)
	}

	// Checked here and kept by the db: validating the caller's slice and reading
	// it again afterwards would let a path that was never checked into the trust
	// set. writePath is not in this check -- ensureFile is about to create it,
	// and newHostKeyDB requires it to be readable once it is there.
	alsoVerify = slices.Clone(alsoVerify)
	if err := requireRegularFiles(alsoVerify); err != nil {
		return nil, err
	}

	mu.Lock()
	defer mu.Unlock()

	if err := ensureFile(writePath); err != nil {
		return nil, err
	}

	knownHosts, err := newHostKeyDB([]string{writePath}, alsoVerify)
	if err != nil {
		return nil, err
	}

	return wrapCallback(knownHosts, writePath, permissive, hash), nil
}

// KnownHostsReadOnlyFileCallback returns a HostKeyCallback that only reads from
// an existing known hosts file — it never creates the file or appends new entries.
// This is appropriate for system-wide files such as /etc/ssh/ssh_known_hosts that
// should not be modified by unprivileged users.
func KnownHostsReadOnlyFileCallback(path string, permissive bool) (ssh.HostKeyCallback, error) {
	if path == devNull {
		return InsecureIgnoreHostKeyCallback, nil
	}

	hkc, err := readOnlyChecker([]string{path})
	if err != nil {
		return nil, err
	}

	return wrapReadOnlyCallback(hkc, permissive), nil
}

// KnownHostsReadOnlyFilesCallback is [KnownHostsReadOnlyFileCallback] over
// several known_hosts files read as one trust set: a key matching an entry in
// any of them is accepted, and a host is unknown only when none of them
// mentions it. That is how OpenSSH reads a user's file together with the
// system-wide ones, and it is what lets an administrator's entry in
// /etc/ssh/ssh_known_hosts count even when the user's own file has never heard
// of the host.
//
// Every path must be an existing regular file. /dev/null is not special here:
// a caller combining trust sources has no use for one that verifies nothing,
// and it is not a regular file, so it is rejected like any other non-file.
func KnownHostsReadOnlyFilesCallback(paths []string, permissive bool) (ssh.HostKeyCallback, error) {
	hkc, err := readOnlyChecker(paths)
	if err != nil {
		return nil, err
	}

	return wrapReadOnlyCallback(hkc, permissive), nil
}

// KnownHostsReadOnlyFilesCallbackWithIPCheck is [KnownHostsReadOnlyFilesCallback]
// with the connecting IP address verified as well, against the same files. The
// IP is checked first; see [WithCheckHostIP] for what that ordering is for.
func KnownHostsReadOnlyFilesCallbackWithIPCheck(paths []string, permissive bool) (ssh.HostKeyCallback, error) {
	hkc, err := readOnlyChecker(paths)
	if err != nil {
		return nil, err
	}

	return wrapCheckHostIP(wrapReadOnlyCallback(hkc, permissive), hkc, permissive), nil
}

// hostKeyDB is a set of known_hosts files parsed into a checker.
//
// knownhosts.New reads the files once and answers from that snapshot ever
// after, which is too little for a callback: the callback outlives the
// connection it was made for, and keys are appended to those files meanwhile --
// by this package, by another callback built over the same files for a
// connection set up alongside this one, or by another process. A decision taken
// from a stale snapshot finds a host that is on file new all over again, and a
// recording policy then stores a second, conflicting key for it: the key change
// accept-new exists to refuse. So refresh rereads the files, and every callback
// here calls it before every decision rather than caching what it last saw.
// Trust is not something to answer from a cache whose invalidation could be
// wrong -- and a parse costs well under a millisecond against a handshake of
// tens.
//
// The files fall in two groups. An optional one that cannot be read when the
// files are read contributes nothing rather than failing the decision, and
// counts again once it can be; one trust source among several going away is not
// reason enough to refuse every connection. A required one is the opposite: it
// has to be read, and a decision is refused rather than taken without it. The
// file a key would be recorded in is required, since a key it already holds is
// the only thing standing between accept-new and recording a second, different
// key for a host it has seen before.
type hostKeyDB struct {
	required []string
	optional []string
	check    ssh.HostKeyCallback
}

// newHostKeyDB parses the files into a checker. The lists are copied: a
// callback is long-lived and rereads them on every decision, so sharing the
// caller's slice would let a later change to it move the trust set out from
// under a verification -- or race with one. Must be called with mu held.
func newHostKeyDB(required, optional []string) (*hostKeyDB, error) {
	db := &hostKeyDB{required: slices.Clone(required), optional: slices.Clone(optional), check: nil}
	if err := db.refresh(); err != nil {
		return nil, err
	}
	return db, nil
}

// refresh rereads the files, replacing the snapshot the checker answers from.
// The old snapshot is kept when the files cannot be parsed, so a checker is
// never left unset. Must be called with mu held.
func (d *hostKeyDB) refresh() error {
	paths := make([]string, 0, len(d.required)+len(d.optional))
	for _, path := range d.required {
		// Not readable, no decision: a required file that cannot be read is not
		// an empty one, and treating it as empty would make every host look new.
		if err := checkReadable(path); err != nil {
			return err
		}
		paths = append(paths, path)
	}
	// knownhosts.New fails on a file it cannot open, which would turn one
	// optional source that went missing or unreadable into a refused
	// connection. Reading what can be read keeps the other sources answering,
	// and a path that comes back joins them again on the next read.
	for _, path := range d.optional {
		if checkReadable(path) == nil {
			paths = append(paths, path)
		}
	}

	check, err := knownhosts.New(paths...)
	if err != nil {
		return fmt.Errorf("%w: knownhosts callback: %w", ErrCheckHostKey, err)
	}
	d.check = check
	return nil
}

// callback verifies a host key against the files as they are now. Must be
// called with mu held.
func (d *hostKeyDB) callback(hostname string, remote net.Addr, key ssh.PublicKey) error {
	if err := d.refresh(); err != nil {
		return err
	}
	return d.check(hostname, remote, key)
}

// readOnlyChecker reads paths as a single trust set, requiring each to be an
// existing regular file. All of them are checked before any is read, so an
// unusable path is reported rather than silently contributing nothing. That is
// a check on what the caller asked for, not a promise about later: a file that
// goes missing afterwards contributes nothing until it comes back.
func readOnlyChecker(paths []string) (*hostKeyDB, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("%w: no known_hosts files to verify against", ErrCheckHostKey)
	}
	// Off the caller's slice first, so the list that is checked is the list that
	// is kept even if the caller goes on to reuse or change its own.
	paths = slices.Clone(paths)
	if err := requireRegularFiles(paths); err != nil {
		return nil, err
	}

	mu.Lock()
	defer mu.Unlock()

	// All optional: nothing here is written to, so a file that goes away leaves
	// the rest of the trust set to answer, and a policy that refuses an unknown
	// host still refuses one when the set is empty.
	return newHostKeyDB(nil, paths)
}

// requireRegularFiles reports the first path that is not an existing regular
// file.
//
// A trust source is worth having only if it is read, and [hostKeyDB] reads what
// is there rather than failing over a file that went missing -- which is right
// once running, but would quietly turn a path the caller asked to verify
// against, and mistyped, into no verification at all. So every caller that
// takes trust sources as an argument checks them here first, before any db is
// built from them.
func requireRegularFiles(paths []string) error {
	for _, path := range paths {
		if err := checkReadable(path); err != nil {
			return err
		}
	}
	return nil
}

// checkReadable reports whether path is a regular file that can be read as a
// known_hosts file.
//
// It opens rather than stats, because that is what the reader does: a file
// whose mode denies it -- 0000, or owned by someone else -- passes every stat
// and then fails knownhosts.New, which takes the whole trust set down with it.
// The mode is read from the open file, so the answer is about the file that was
// opened rather than about whatever the path pointed at a moment earlier.
func checkReadable(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrCheckHostKey, err)
	}
	defer func() { _ = f.Close() }()

	stat, err := f.Stat()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrCheckHostKey, err)
	}
	if !stat.Mode().IsRegular() {
		return fmt.Errorf("%w: %s is not a regular file", ErrCheckHostKey, path)
	}
	return nil
}

// devNullTrustSet builds the callback for a write target of /dev/null, where
// alsoVerify is the whole of the trust set.
//
// The null device holds no entries and keeps none, so what it takes away is the
// record, not the verification: a host that no file in alsoVerify knows is
// accepted and then forgotten -- which is all that appending to /dev/null
// amounts to -- while a key one of them contradicts is still a mismatch. Any
// other reading would let an empty write target overrule a system-wide file.
//
// With nothing left to verify against there is no trust set at all, and the
// null device means what it usually does: no host key verification.
func devNullTrustSet(alsoVerify []string, permissive, checkIP bool) (ssh.HostKeyCallback, error) {
	if len(alsoVerify) == 0 {
		return InsecureIgnoreHostKeyCallback, nil
	}

	hkc, err := readOnlyChecker(alsoVerify)
	if err != nil {
		return nil, err
	}

	callback := AcceptUnknownHosts(wrapReadOnlyCallback(hkc, permissive))
	if checkIP {
		callback = wrapCheckHostIP(callback, hkc, permissive)
	}
	return callback, nil
}

// KnownHostsFileCallbackWithIPCheck is like KnownHostsFileCallback but also
// verifies the connecting IP address, against the same file. The IP is checked
// first; see [WithCheckHostIP] for what that ordering is for.
func KnownHostsFileCallbackWithIPCheck(path string, permissive, hash bool) (ssh.HostKeyCallback, error) {
	return KnownHostsFilesCallbackWithIPCheck(path, nil, permissive, hash)
}

// KnownHostsFilesCallbackWithIPCheck is [KnownHostsFilesCallback] with the
// connecting IP address verified as well, against the same files. The IP is
// checked first; see [WithCheckHostIP] for what that ordering is for.
func KnownHostsFilesCallbackWithIPCheck(writePath string, alsoVerify []string, permissive, hash bool) (ssh.HostKeyCallback, error) {
	if writePath == devNull {
		return devNullTrustSet(alsoVerify, permissive, true)
	}

	// Checked here and kept by the db: validating the caller's slice and reading
	// it again afterwards would let a path that was never checked into the trust
	// set. writePath is not in this check -- ensureFile is about to create it,
	// and newHostKeyDB requires it to be readable once it is there.
	alsoVerify = slices.Clone(alsoVerify)
	if err := requireRegularFiles(alsoVerify); err != nil {
		return nil, err
	}

	mu.Lock()
	defer mu.Unlock()

	if err := ensureFile(writePath); err != nil {
		return nil, err
	}

	knownHosts, err := newHostKeyDB([]string{writePath}, alsoVerify)
	if err != nil {
		return nil, err
	}

	return wrapCheckHostIP(wrapCallback(knownHosts, writePath, permissive, hash), knownHosts, permissive), nil
}

// KnownHostsReadOnlyFileCallbackWithIPCheck is like KnownHostsReadOnlyFileCallback
// but also verifies the connecting IP address, against the same file. The IP is
// checked first; see [WithCheckHostIP] for what that ordering is for.
func KnownHostsReadOnlyFileCallbackWithIPCheck(path string, permissive bool) (ssh.HostKeyCallback, error) {
	if path == devNull {
		return InsecureIgnoreHostKeyCallback, nil
	}

	return KnownHostsReadOnlyFilesCallbackWithIPCheck([]string{path}, permissive)
}

// wrapCallback extends a knownhosts callback to record the key of a host that
// is not in the known_hosts file yet and accept it, instead of returning an
// error. A host whose recorded key has changed is still a mismatch.
//
// That is OpenSSH's StrictHostKeyChecking=accept-new. The other modes are
// composed from the pieces here rather than being options on this callback:
// "yes" is [KnownHostsReadOnlyFileCallback], whose refusal to record a key is
// the whole of what strict means, and "no" is the permissive flag, which
// downgrades a mismatch to a warning on stderr. "ask" has no meaning without a
// terminal to ask at, so a caller with no answer to give picks between
// accept-new and yes.
func wrapCallback(knownHosts *hostKeyDB, path string, permissive, hash bool) ssh.HostKeyCallback {
	return ssh.HostKeyCallback(func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		mu.Lock()
		defer mu.Unlock()

		// The files as they are now, not as they were when this callback was
		// built: a key recorded meanwhile -- by this callback on an earlier
		// connection, by another callback over the same files, or by another
		// process -- has to count here too, or the host reads as new again and
		// gets a second, conflicting key recorded for it.
		err := knownHosts.callback(hostname, remote, key)
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

// wrapReadOnlyCallback wraps a knownhosts checker to reject unknown hosts
// without writing to the file. When permissive is true, unknown hosts are
// accepted silently instead of rejected.
func wrapReadOnlyCallback(knownHosts *hostKeyDB, permissive bool) ssh.HostKeyCallback {
	return ssh.HostKeyCallback(func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		mu.Lock()
		defer mu.Unlock()
		err := knownHosts.callback(hostname, remote, key)
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

// AcceptUnknownHosts wraps cb so that a host it has never seen is accepted
// rather than refused, while a host whose recorded key has changed remains a
// mismatch. Any other error is passed through untouched.
//
// This is StrictHostKeyChecking=accept-new applied to a trust source that
// cannot be written to, such as a system-wide known_hosts file: the key is
// accepted but not recorded, so the next connection reaches the same decision
// again rather than pinning what it saw the first time. Where a writable user
// file exists, [KnownHostsFileCallback] is the better fit, since it records the
// key and so can detect a later change.
func AcceptUnknownHosts(cb ssh.HostKeyCallback) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := cb(hostname, remote, key)
		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) && len(keyErr.Want) == 0 {
			// An empty Want is knownhosts' way of saying the host is not on
			// file at all, as opposed to being on file under another key.
			return nil
		}
		return err
	}
}

// WithCheckHostIP wraps callback to also verify the connecting IP address in
// known_hosts. When the remote address is a TCP connection the actual connected
// IP is checked directly; otherwise all DNS-resolved addresses are checked.
// If the IP is found in known_hosts with a different key (potential DNS
// spoofing), ErrHostKeyMismatch is returned. DNS resolution failures are
// non-fatal. Skipped when hostname is already an IP address. Unlike OpenSSH,
// this implementation never writes IP addresses to known_hosts.
//
// The IP is checked before callback runs, so a recording callback cannot store
// a key the file already contradicts under the connecting IP. The two checks
// read the files separately, each seeing them as they are when it runs, rather
// than sharing one read: they are not one atomic look at known_hosts, and an
// entry written between them belongs to whichever check follows it. That is the
// same race as an entry written just before the callback, and it is decided the
// same way -- by the next connection, which reads the file again.
func WithCheckHostIP(callback ssh.HostKeyCallback, path string, permissive bool) (ssh.HostKeyCallback, error) {
	if path == devNull {
		return callback, nil
	}
	// Through readOnlyChecker, so a path that cannot be read is reported here
	// rather than quietly becoming an empty trust set -- a caller that asked for
	// the IP to be checked would otherwise get a callback that checks nothing.
	knownHosts, err := readOnlyChecker([]string{path})
	if err != nil {
		return nil, fmt.Errorf("check-host-ip: %w", err)
	}
	return wrapCheckHostIP(callback, knownHosts, permissive), nil
}

// checkResolvedIP checks a single resolved IP address against knownHosts.
// Returns an error for a known key mismatch (potential DNS spoofing) and, in
// strict mode, also for unexpected checker errors (IO, address parsing).
// Must be called with mu held.
func checkResolvedIP(knownHosts *hostKeyDB, addr, port string, remote net.Addr, key ssh.PublicKey, permissive bool) error {
	effectivePort := port
	ipAddr := &net.TCPAddr{IP: net.ParseIP(addr)}
	if tcp, ok := remote.(*net.TCPAddr); ok {
		ipAddr.Port = tcp.Port
		effectivePort = strconv.Itoa(tcp.Port)
	} else if p, err := strconv.Atoi(port); err == nil {
		ipAddr.Port = p
	}
	ipHost := net.JoinHostPort(addr, effectivePort)
	// Through the db, so the IP side of the check reads the files as they are
	// now as well -- an entry added for the IP since this callback was built
	// contradicts a spoofed key only if it is actually read.
	ipErr := knownHosts.callback(ipHost, ipAddr, key)
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

func wrapCheckHostIP(callback ssh.HostKeyCallback, knownHosts *hostKeyDB, permissive bool) ssh.HostKeyCallback {
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
			if ipErr := checkHostIP(knownHosts, host, port, remote, key, permissive); ipErr != nil {
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
func checkHostIP(knownHosts *hostKeyDB, host, port string, remote net.Addr, key ssh.PublicKey, permissive bool) error {
	// When the connected address is known, check only that IP to avoid
	// false positives from multi-homed hostnames (round-robin/CDN).
	if tcp, ok := remote.(*net.TCPAddr); ok && tcp.IP != nil {
		mu.Lock()
		err := checkResolvedIP(knownHosts, tcp.IP.String(), port, remote, key, permissive)
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
		if loopErr = checkResolvedIP(knownHosts, addr, port, remote, key, permissive); loopErr != nil {
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
