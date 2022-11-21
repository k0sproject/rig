// Package errstring defines a simple error struct
package errstring

import (
	"fmt"
)

// Error is the base type for rig errors
type Error struct {
	msg string
}

// Error implements the error interface
func (e *Error) Error() string {
	return e.msg
}

func (e *Error) Unwrap() error {
	return nil
}

// New creates a new error
func New(msg string) *Error {
	return &Error{msg}
}

// Wrap wraps another error with this error
func (e *Error) Wrap(errB error) error {
	return &wrappedError{
		errA: e,
		errB: errB,
	}
}

// Wrapf is a shortcut for Wrap(fmt.Errorf("...", ...))
func (e *Error) Wrapf(msg string, args ...any) error {
	return &wrappedError{
		errA: e,
		errB: fmt.Errorf(msg, args...), //nolint:goerr113
	}
}

type wrappedError struct {
	errA error
	errB error
}

func (e *wrappedError) Error() string {
	return e.errA.Error() + ": " + e.errB.Error()
}

func (e *wrappedError) Is(err error) bool {
	if err == nil {
		return false
	}
	return e.errA == err //nolint:goerr113
}

func (e *wrappedError) Unwrap() error {
	return e.errB
}
