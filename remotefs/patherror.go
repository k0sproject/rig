package remotefs

import (
	"errors"
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
	return PathError(op, path, fmt.Errorf(template, args...)) //nolint:err113
}

// pathErrorCause unwraps a *fs.PathError so that an operation implemented on
// top of another can report itself without nesting a second one. Nesting gets
// the outer Op right but repeats the path in the message, as in
// "remove C:\\x: stat C:\\x: ...".
func pathErrorCause(err error) error {
	if pathErr, ok := errors.AsType[*fs.PathError](err); ok {
		return pathErr.Err
	}
	return err
}
