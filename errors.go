package rig

import (
	"github.com/k0sproject/rig/errstring"
)

var (
	ErrOS               = errstring.New("local os")              // ErrOS is returned when an action fails on local OS
	ErrInvalidPath      = errstring.New("invalid path")          // ErrInvalidPath is returned when a path is invalid
	ErrValidationFailed = errstring.New("validation failed")     // ErrValidationFailed is returned when a validation fails
	ErrSudoRequired     = errstring.New("sudo required")         // ErrSudoRequired is returned when sudo is required
	ErrNotFound         = errstring.New("not found")             // ErrNotFound is returned when a resource is not found
	ErrNotImplemented   = errstring.New("not implemented")       // ErrNotImplemented is returned when a feature is not implemented
	ErrNotSupported     = errstring.New("not supported")         // ErrNotSupported is returned when a feature is not supported
	ErrAuthFailed       = errstring.New("authentication failed") // ErrAuthFailed is returned when authentication fails
	ErrUploadFailed     = errstring.New("upload failed")         // ErrUploadFailed is returned when an upload fails
	ErrNotConnected     = errstring.New("not connected")         // ErrNotConnected is returned when a connection is not established
	ErrCantConnect      = errstring.New("can't connect")         // ErrCantConnect is returned when a connection is not established and retrying will fail
	ErrCommandFailed    = errstring.New("command failed")        // ErrCommandFailed is returned when a command fails
)
