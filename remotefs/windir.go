package remotefs

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/k0sproject/rig/exec"
)

var (
	_ fs.ReadDirFile = (*winDir)(nil)
	_ File           = (*winDir)(nil)
)

// winDir is a directory on a Windows target. It implements fs.ReadDirFile.
type winDir struct {
	winFileDirBase
	buffer *dirEntryBuffer
}

func (f *winDir) Read(_ []byte) (int, error) {
	return 0, f.pathErr("read", fmt.Errorf("%w: is a directory", fs.ErrInvalid))
}

func (f *winDir) Seek(_ int64, _ int) (int64, error) {
	return 0, f.pathErr("seek", fmt.Errorf("%w: is a directory", fs.ErrInvalid))
}

func (f *winDir) Write(_ []byte) (int, error) {
	return 0, f.pathErr("write", fmt.Errorf("%w: is a directory", fs.ErrInvalid))
}

func (f *winDir) CopyTo(_ io.Writer) (int64, error) {
	return 0, f.pathErr("write", fmt.Errorf("%w: is a directory", fs.ErrInvalid))
}

func (f *winDir) CopyFrom(_ io.Reader) (int64, error) {
	return 0, f.pathErr("write", fmt.Errorf("%w: is a directory", fs.ErrInvalid))
}

func (f *winDir) Close() error {
	if f.closed {
		return f.pathErr("close", fs.ErrClosed)
	}
	f.closed = true
	return nil
}

var statDirTemplate = `
$items = Get-ChildItem -LiteralPath %s | Select-Object Name, FullName, LastWriteTime, Attributes, Mode, Length | ForEach-Object {
    $isReadOnly = [bool]($_.Attributes -band [System.IO.FileAttributes]::ReadOnly)
    $_ | Add-Member -NotePropertyName IsReadOnly -NotePropertyValue $isReadOnly -PassThru 
}
if ($items -eq $null) { 
	throw "does not exist"
}
ConvertTo-Json -Compress -Depth 5 @($items)
`

// ReadDir reads the contents of the directory and returns
// a slice of up to n fs.DirEntry values in directory order.
// Subsequent calls on the same file will yield further DirEntry values.
func (f *winDir) ReadDir(n int) ([]fs.DirEntry, error) {
	if f.buffer == nil {
		out, err := f.fs.ExecOutput(statDirTemplate, f.path, exec.PS())
		if err != nil {
			return nil, fmt.Errorf("readdir: %w", err)
		}
		var fileinfos []*winFileInfo
		if err := json.Unmarshal([]byte(out), &fileinfos); err != nil {
			return nil, fmt.Errorf("decode readdir output: %w", err)
		}
		entries := make([]fs.DirEntry, len(fileinfos))
		for i, info := range fileinfos {
			entries[i] = info
		}
		f.buffer = newDirEntryBuffer(entries)
	}
	return f.buffer.Next(n)
}

func (f *winDir) open(flags int) error {
	if f.closed {
		return f.pathErr("open", fs.ErrClosed)
	}

	if flags&(os.O_WRONLY|os.O_RDWR|os.O_APPEND|os.O_CREATE|os.O_TRUNC|os.O_EXCL) != 0 {
		return f.pathErr("open", fmt.Errorf("%w: incompatible flags for directory access", fs.ErrInvalid))
	}

	return nil
}
