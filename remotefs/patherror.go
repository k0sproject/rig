package remotefs

import (
	"fmt"
	"io/fs"
)

// PathError returns a fs.PathError with the given operation, path and error.
func PathError(op, path string, err error) *fs.PathError {
	return &fs.PathError{Op: op, Path: path, Err: err}
}

// PathErrorf returns a fs.PathError with the given operation, path and error created using a
// sprintf style format string and arguments.
func PathErrorf(op, path string, template string, args ...any) *fs.PathError {
	return PathError(op, path, fmt.Errorf(template, args...)) //nolint:goerr113
}
