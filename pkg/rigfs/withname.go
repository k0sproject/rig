package rigfs

import "io/fs"

const (
	OpClose = "close" // OpClose Close operation
	OpOpen  = "open"  // OpOpen Open operation
	OpRead  = "read"  // OpRead Read operation
	OpSeek  = "seek"  // OpSeek Seek operation
	OpStat  = "stat"  // OpStat Stat operation
	OpWrite = "write" // OpWrite Write operation
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
