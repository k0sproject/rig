package rigfs

import "io/fs"

// File operation names for use in error handling.
const (
	// OpClose represents a close operation.
	OpClose = "close"
	// OpOpen represents an open operation.
	OpOpen = "open"
	// OpRead represents a read operation.
	OpRead = "read"
	// OpSeek represents a seek operation.
	OpSeek = "seek"
	// OpStat represents a stat operation.
	OpStat = "stat"
	// OpWrite represents a write operation.
	OpWrite = "write"
	// OpCopyTo represents a copy-to operation.
	OpCopyTo = "copy-to"
	// OpCopyFrom represents a copy-from operation.
	OpCopyFrom = "copy-from"
)

type withPath struct {
	path string
}

func (w *withPath) Name() string {
	return w.path
}

func (w *withPath) pathErr(op string, err error) error {
	return &fs.PathError{Op: op, Path: w.path, Err: err}
}
