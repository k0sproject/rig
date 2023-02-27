package rig

import "errors"

var (
	ErrOS               = errors.New("local os")              // ErrOS is returned when an action fails on local OS
	ErrInvalidPath      = errors.New("invalid path")          // ErrInvalidPath is returned when a path is invalid
	ErrValidationFailed = errors.New("validation failed")     // ErrValidationFailed is returned when a validation fails
	ErrSudoRequired     = errors.New("sudo required")         // ErrSudoRequired is returned when sudo is required
	ErrNotFound         = errors.New("not found")             // ErrNotFound is returned when a resource is not found
	ErrNotImplemented   = errors.New("not implemented")       // ErrNotImplemented is returned when a feature is not implemented
	ErrNotSupported     = errors.New("not supported")         // ErrNotSupported is returned when a feature is not supported
	ErrAuthFailed       = errors.New("authentication failed") // ErrAuthFailed is returned when authentication fails
	ErrUploadFailed     = errors.New("upload failed")         // ErrUploadFailed is returned when an upload fails
	ErrNotConnected     = errors.New("not connected")         // ErrNotConnected is returned when a connection is not established
	ErrCantConnect      = errors.New("can't connect")         // ErrCantConnect is returned when a connection is not established and retrying will fail
	ErrCommandFailed    = errors.New("command failed")        // ErrCommandFailed is returned when a command fails
)
