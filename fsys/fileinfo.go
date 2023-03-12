package fsys

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"time"
)

// Check interfaces
var (
	_ fs.FileInfo = (*FileInfo)(nil)
	_ fs.DirEntry = (*FileInfo)(nil)
)

// FileInfo implements fs.FileInfo for stat on remote files
type FileInfo struct {
	FName    string      `json:"name"`
	FSize    int64       `json:"size"`
	FMode    fs.FileMode `json:"mode"`
	FUnix    fs.FileMode `json:"unixMode"`
	FModTime time.Time   `json:"-"`
	FIsDir   bool        `json:"isDir"`
	ModtimeS int64       `json:"modTime"`
	fsys     fs.FS
}

// UnmarshalJSON implements json.Unmarshaler
func (f *FileInfo) UnmarshalJSON(b []byte) error {
	type fileInfo *FileInfo
	fi := fileInfo(f)
	if err := json.Unmarshal(b, fi); err != nil {
		return fmt.Errorf("unmarshal fileinfo: %w", err)
	}
	f.FModTime = time.Unix(f.ModtimeS, 0)
	f.FName = strings.ReplaceAll(f.FName, "\\", "/")

	var newmode fs.FileMode

	if f.FMode&1 != 0 { // "Readonly"
		newmode = 0o444
	}
	if f.FMode&16 != 0 { // "Directory"
		newmode |= fs.ModeDir | 0o544
	}
	if f.FMode&64 != 0 { // "Device"
		newmode |= fs.ModeCharDevice
	}
	if f.FMode&4096 != 0 { // "Offline"
		newmode |= fs.ModeIrregular
	}
	if f.FMode&1024 != 0 { // "ReparsePoint"
		newmode |= fs.ModeIrregular
		newmode |= fs.ModeSymlink
	}
	if f.FMode&256 != 0 { // "Temporary"
		newmode |= fs.ModeTemporary
	}
	f.FMode = newmode
	return nil
}

// Name returns the file name
func (f *FileInfo) Name() string {
	return path.Base(f.FName)
}

// FullPath returns the full path
func (f *FileInfo) FullPath() string {
	return f.FName
}

// Size returns the file size
func (f *FileInfo) Size() int64 {
	return f.FSize
}

// Mode returns the file permission mode
func (f *FileInfo) Mode() fs.FileMode {
	if f.FUnix != 0 {
		return f.FUnix
	}
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

// Sys returns the underlying data source
func (f *FileInfo) Sys() any {
	return f.fsys
}

// Type returns the file type. It's here to satisfy fs.DirEntry interface.
func (f *FileInfo) Type() fs.FileMode {
	return f.Mode().Type()
}

// Info returns self. It's here to satisfy fs.DirEntry interface.
func (f *FileInfo) Info() (fs.FileInfo, error) {
	return f, nil
}
