// Package os provides a platform-independent interface to operating system functionality.
package os

import (
	"io/fs"
	"time"
)

// FileInfo implements fs.FileInfo for stat on remote files
type FileInfo struct {
	FName    string
	FSize    int64
	FMode    fs.FileMode
	FModTime time.Time
	FIsDir   bool
}

// Name returns the file name
func (f *FileInfo) Name() string {
	return f.FName
}

// Size returns the file size
func (f *FileInfo) Size() int64 {
	return f.FSize
}

// Mode returns the file permission mode
func (f *FileInfo) Mode() fs.FileMode {
	return f.FMode
}

// ModTime returns the last modification time of a file
func (f *FileInfo) ModTime() time.Time {
	return f.FModTime
}

// IsDir returns true if the file path points to a directory
func (f *FileInfo) IsDir() bool {
	return f.FIsDir
}
