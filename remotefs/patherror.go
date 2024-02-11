package remotefs

import (
	"fmt"
	"io/fs"
)

func PathError(op, path string, err error) *fs.PathError {
	return &fs.PathError{Op: op, Path: path, Err: err}
}

func PathErrorf(op, path string, template string, args ...any) *fs.PathError {
	return PathError(op, path, fmt.Errorf(template, args...))
}
