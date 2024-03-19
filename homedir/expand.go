//go:build !windows

package homedir

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

var errNotImplemented = errors.New("not implemented")

// Expand does ~/ style path expansion for files under current user home. ~user/ style paths are not supported.
func Expand(dir string) (string, error) {
	if !strings.HasPrefix(dir, "~") {
		return dir, nil
	}

	parts := strings.Split(dir, string(os.PathSeparator))
	if parts[0] != "~" {
		return "", fmt.Errorf("%w: ~user/ style paths not supported", errNotImplemented)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("homedir expand: %w", err)
	}
	home = strings.ReplaceAll(filepath.Clean(home), "\\", "/")

	parts[0] = home

	return path.Join(parts...), nil
}
