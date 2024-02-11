package remotefs

import "io/fs"

var _ fs.ReadDirFile = (*PosixDir)(nil)

// PosixDir implements fs.ReadDirFile for a remote directory
type PosixDir struct {
	PosixFile
	buffer *dirEntryBuffer
}

// ReadDir returns a list of directory entries
func (f *PosixDir) ReadDir(n int) ([]fs.DirEntry, error) {
	if f.buffer == nil {
		entries, err := f.fs.ReadDir(f.path)
		if err != nil {
			return nil, err
		}
		f.buffer = newDirEntryBuffer(entries)
	}
	return f.buffer.Next(n)
}
