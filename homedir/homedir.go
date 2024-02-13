// Package homedir provides functions for getting the user's home directory in Go.
package homedir

import (
	"errors"
	"fmt"
	"os"
)

var (
	errNotImplemented = errors.New("not implemented")
	// ErrInvalidPath is returned when the given path is invalid.
	ErrInvalidPath = errors.New("invalid path")
)

// Home returns the home directory for the executing user.
func Home() (string, error) {
	if home, ok := os.LookupEnv("HOME"); ok {
		return home, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get user home directory: %w", err)
	}
	return home, nil
}

// Expand does ~/ style path expansion for files under current user home. ~user/ style paths are not supported.
func Expand(path string) (string, error) {
	if path[0] != '~' {
		return path, nil
	}
	if len(path) == 1 {
		return Home()
	}
	if path[1] != '/' {
		return "", fmt.Errorf("%w: ~user/ style paths not supported", errNotImplemented)
	}

	home, err := Home()
	if err != nil {
		return "", err
	}
	return home + path[1:], nil
}

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
