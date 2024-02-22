package remotefs

import (
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"time"
)

var _ fs.FileInfo = (*winFileInfo)(nil)
var _ fs.DirEntry = (*winFileInfo)(nil)

type windowsFileInfoTime time.Time

func (t *windowsFileInfoTime) UnmarshalJSON(b []byte) error {
	strTime := strings.Trim(string(b), "\"\\/Date()")
	milliseconds, err := strconv.ParseInt(strTime, 10, 64)
	if err != nil {
		return fmt.Errorf("decode time: %w", err)
	}

	seconds := milliseconds / 1000
	nanoseconds := (milliseconds % 1000) * 1000000
	*t = windowsFileInfoTime(time.Unix(seconds, nanoseconds).Truncate(time.Millisecond))
	return nil
}

type winFileInfo struct {
	Path          string              `json:"Name"`
	Length        int64               `json:"Length"`
	IsReadOnly    bool                `json:"IsReadOnly"`
	FullName      string              `json:"FullName"`
	Extension     string              `json:"Extension"`
	LastWriteTime windowsFileInfoTime `json:"LastWriteTime"`
	Attributes    int                 `json:"Attributes"`
	FMode         string              `json:"Mode"`
	Err           string              `json:"Err"`
	fs            *WinFS
}

// Name returns the base name of the file.
func (fi *winFileInfo) Name() string {
	parts := strings.Split(fi.Path, "\\")
	return parts[len(parts)-1]
}

// Size returns the length in bytes for regular files; system-dependent for others.
func (fi *winFileInfo) Size() int64 {
	return fi.Length
}

// Mode returns the file mode bits.
func (fi *winFileInfo) Mode() fs.FileMode {
	if fi.IsReadOnly {
		return 0o555
	}
	return 0o777
}

// ModTime returns the modification time.
func (fi *winFileInfo) ModTime() time.Time {
	return time.Time(fi.LastWriteTime)
}

// IsDir is abbreviation for Mode().IsDir().
func (fi *winFileInfo) IsDir() bool {
	return strings.Contains(fi.FMode, "d")
}

// Sys returns the underlying data source (can return nil).
func (fi *winFileInfo) Sys() any {
	return fi.fs
}

// Info returns self, satisfying fs.DirEntry interface.
func (fi *winFileInfo) Info() (fs.FileInfo, error) {
	return fi, nil
}

// Type returns the type bits for the entry.
// The type bits are a subset of the usual FileMode bits, those returned by the FileMode.Type method.
func (fi *winFileInfo) Type() fs.FileMode {
	return fi.Mode().Type()
}
