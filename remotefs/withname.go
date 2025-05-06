package remotefs

import "io/fs"

const (
	// OpClose Close operation.
	OpClose = "close"
	// OpOpen Open operation.
	OpOpen = "open"
	// OpRead Read operation.
	OpRead = "read"
	// OpSeek Seek operation.
	OpSeek = "seek"
	// OpStat Stat operation.
	OpStat = "stat"
	// OpWrite Write operation.
	OpWrite = "write"
	// OpCopyTo CopyTo operation.
	OpCopyTo = "copy-to"
	// OpCopyFrom CopyFrom operation.
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
