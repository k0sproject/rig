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
	// InsecureIgnoreHostKeyCallback is an insecure HostKeyCallback that accepts any host key.
	InsecureIgnoreHostKeyCallback = ssh.InsecureIgnoreHostKey() //nolint:gosec

	// ErrHostKeyMismatch is returned when the host key does not match the host key or a key in known_hosts file
	ErrHostKeyMismatch = errors.New("host key mismatch")

	// ErrInvalidPath is returned for unusable file paths
	ErrInvalidPath = errors.New("invalid path")

	// DefaultKnownHostsPath is the default path to the known_hosts file - make sure to homedir-expand it
	DefaultKnownHostsPath = "~/.ssh/known_hosts2"

	mu sync.Mutex
)

// StaticKeyCallback returns a HostKeyCallback that checks the host key against a given host key
func StaticKeyCallback(trustedKey string) ssh.HostKeyCallback {
	return func(_ string, _ net.Addr, k ssh.PublicKey) error {
		ks := keyString(k)
		if trustedKey != ks {
			return ErrHostKeyMismatch
		}

		return nil
	}
}

// KnownHostsPathFromEnv returns the path to a known_hosts file from the environment variable SSH_KNOWN_HOSTS
var KnownHostsPathFromEnv = func() (string, bool) {
	return os.LookupEnv("SSH_KNOWN_HOSTS")
}

// KnownHostsFileCallback returns a HostKeyCallback that uses a known hosts file to verify host keys
func KnownHostsFileCallback(path string) (ssh.HostKeyCallback, error) {
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
		return nil, fmt.Errorf("create knownhosts callback: %w", err)
	}

	return wrapCallback(hkc, path), nil
}

// extends a knownhosts callback to not return an error when the key
// is not found in the known_hosts file but instead adds it to the file as new
// entry
func wrapCallback(hkc ssh.HostKeyCallback, path string) ssh.HostKeyCallback {
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
			return fmt.Errorf("%w: %s", ErrHostKeyMismatch, err)
		}

		dbFile, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return fmt.Errorf("failed to open ssh known_hosts file %s for writing: %w", path, err)
		}

		knownHostsEntry := knownhosts.Normalize(remote.String())
		row := knownhosts.Line([]string{knownHostsEntry}, key)
		row = fmt.Sprintf("%s\n", strings.TrimSpace(row))

		if _, err := dbFile.WriteString(row); err != nil {
			return fmt.Errorf("failed to write to known hosts file %s: %w", path, err)
		}
		if err := dbFile.Close(); err != nil {
			return fmt.Errorf("failed to close known_hosts file after writing: %w", err)
		}
		return nil
	})
}

func fileExists(path string) bool {
	stat, err := os.Stat(path)
	return err == nil && stat.Mode().IsRegular()
}

func ensureDir(path string) error {
	stat, err := os.Stat(path)
	if err == nil && !stat.Mode().IsDir() {
		return fmt.Errorf("%w: path %s is not a directory", ErrInvalidPath, path)
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

// create human-readable SSH-key strings e.g. "ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTY...."
func keyString(k ssh.PublicKey) string {
	return k.Type() + " " + base64.StdEncoding.EncodeToString(k.Marshal())
}
