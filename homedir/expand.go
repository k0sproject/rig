//go:build !windows

// Package homedir provides functions for getting the user's home directory in Go.
package homedir

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var errNotImplemented = errors.New("not implemented")

// Expand does ~/ style path expansion for files under current user home. ~user/ style paths are not supported.
func Expand(path string) (string, error) {
	if !strings.HasPrefix(path, "~") {
		return path, nil
	}

	parts := strings.Split(path, string(os.PathSeparator))
	if parts[0] != "~" {
		return "", fmt.Errorf("%w: ~user/ style paths not supported", errNotImplemented)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("homedir expand: %w", err)
	}

	parts[0] = home

	return filepath.Join(parts...), nil
}
