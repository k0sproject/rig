// Package hostkey implements a callback for the ssh.ClientConfig.HostKeyCallback
package hostkey

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

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
	if path == "/dev/null" {
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
	if path == "/dev/null" {
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

// extends a knownhosts callback to not return an error when the key
// is not found in the known_hosts file but instead adds it to the file as new
// entry.
func wrapCallback(hkc ssh.HostKeyCallback, path string, permissive, hash bool) ssh.HostKeyCallback {
	// TODO this should use the HostKeyAlias from the ssh config. It should also support the
	// "accept-new" of StrictHostNameChecking and possibly some other options.
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

// create human-readable SSH-key strings e.g. "ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTY....".
func keyString(k ssh.PublicKey) string {
	return k.Type() + " " + base64.StdEncoding.EncodeToString(k.Marshal())
}
