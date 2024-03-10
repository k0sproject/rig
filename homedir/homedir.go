// Package homedir provides functions for expanding paths like ~/.ssh.
package homedir

import (
	"errors"
	"fmt"
	"os"
)

// ErrInvalidPath is returned when the given path is invalid.
var ErrInvalidPath = errors.New("invalid path")

func expandStat(path string) (os.FileInfo, error) {
	if len(path) == 0 {
		return nil, fmt.Errorf("%w: path is empty", ErrInvalidPath)
	}
	path, err := Expand(path)
	if err != nil {
		return nil, err
	}
	stat, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat: %w", err)
	}
	return stat, nil
}

// ExpandFile expands the path and checks that it is an existing file.
func ExpandFile(path string) (string, error) {
	stat, err := expandStat(path)
	if err != nil {
		return "", fmt.Errorf("file does not exist: %w", err)
	}

	if stat.IsDir() {
		return "", fmt.Errorf("%w: %s is a directory", ErrInvalidPath, path)
	}

	return path, nil
}

// ExpandDir expands the path and checks that it is an existing directory.
func ExpandDir(path string) (string, error) {
	stat, err := expandStat(path)
	if err != nil {
		return "", fmt.Errorf("directory does not exist: %w", err)
	}

	if !stat.IsDir() {
		return "", fmt.Errorf("%w: %s is not a directory", ErrInvalidPath, path)
	}

	return path, nil
}
