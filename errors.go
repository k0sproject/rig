package rig

import "errors"

// Common error sentinel values.
var (
	// ErrOS is returned when an action fails on local OS.
	ErrOS = errors.New("local os")
	// ErrInvalidPath is returned when a path is invalid.
	ErrInvalidPath = errors.New("invalid path")
	// ErrValidationFailed is returned when a validation fails.
	ErrValidationFailed = errors.New("validation failed")
	// ErrSudoRequired is returned when sudo is required.
	ErrSudoRequired = errors.New("sudo required")
	// ErrNotFound is returned when a resource is not found.
	ErrNotFound = errors.New("not found")
	// ErrNotImplemented is returned when a feature is not implemented.
	ErrNotImplemented = errors.New("not implemented")
	// ErrNotSupported is returned when a feature is not supported.
	ErrNotSupported = errors.New("not supported")
	// ErrAuthFailed is returned when authentication fails.
	ErrAuthFailed = errors.New("authentication failed")
	// ErrUploadFailed is returned when an upload fails.
	ErrUploadFailed = errors.New("upload failed")
	// ErrNotConnected is returned when a connection is not established.
	ErrNotConnected = errors.New("not connected")
	// ErrCantConnect is returned when a connection is not established and retrying will fail.
	ErrCantConnect = errors.New("can't connect")
	// ErrCommandFailed is returned when a command fails.
	ErrCommandFailed = errors.New("command failed")
)
