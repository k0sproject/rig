package rigfs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/k0sproject/rig/exec"
	ps "github.com/k0sproject/rig/pkg/powershell"
)

var _ fs.FS = (*WinFsys)(nil)

// WinFsys is a fs.FS implemen{
type WinFsys struct {
	exec.Runner
}

// NewWindowsFsys returns a new fs.FS implementing filesystem for Windows targets
func NewWindowsFsys(conn exec.Runner) *WinFsys {
	return &WinFsys{conn}
}

var statCmdTemplate = `if (Test-Path -LiteralPath %[1]s) {
		$item = Get-Item -LiteralPath %[1]s | Select-Object Name, FullName, LastWriteTime, Attributes, Mode, Length | ForEach-Object {
			$isReadOnly = [bool]($_.Attributes -band [System.IO.FileAttributes]::ReadOnly)
			$_ | Add-Member -NotePropertyName IsReadOnly -NotePropertyValue $isReadOnly -PassThru 
		}
	  $item | ConvertTo-Json -Compress
	} else {
		Write-Output '{"Err":"does not exist"}'
	}`

// Stat returns fs.FileInfo for the remote file.
func (fsys *WinFsys) Stat(name string) (fs.FileInfo, error) {
	out, err := fsys.ExecOutput(statCmdTemplate, ps.DoubleQuotePath(name), exec.PS())
	if err != nil {
		return nil, &fs.PathError{Op: OpStat, Path: name, Err: fmt.Errorf("%w: %w", err, fs.ErrNotExist)}
	}

	fi := &winFileInfo{fsys: fsys}
	if err := json.Unmarshal([]byte(out), fi); err != nil {
		return nil, &fs.PathError{Op: OpStat, Path: name, Err: fmt.Errorf("%w: stat (parse)", err)}
	}
	if fi.Err != "" {
		if strings.Contains(fi.Err, "does not exist") {
			return nil, &fs.PathError{Op: OpStat, Path: name, Err: fs.ErrNotExist}
		}
		return nil, &fs.PathError{Op: OpStat, Path: name, Err: fmt.Errorf("stat: %v", fi.Err)} //nolint:goerr113
	}
	return fi, nil
}

// Sha256 returns the SHA256 hash of the remote file.
func (fsys *WinFsys) Sha256(name string) (string, error) {
	sum, err := fsys.ExecOutput("(Get-FileHash %s -Algorithm SHA256).Hash.ToLower()", ps.DoubleQuotePath(name), exec.PS())
	if err != nil {
		return "", &fs.PathError{Op: "sum", Path: name, Err: fmt.Errorf("sha256sum: %w", err)}
	}
	return sum, nil
}

// ReadDir reads the directory named by dirname and returns a list of directory entries.
func (fsys *WinFsys) ReadDir(name string) ([]fs.DirEntry, error) {
	f, err := fsys.OpenFile(name, os.O_RDONLY, 0)
	if err != nil {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: err}
	}
	defer f.Close()
	dir, ok := f.(*winDir)
	if !ok {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fmt.Errorf("readdir: %w", fs.ErrInvalid)}
	}

	return dir.ReadDir(-1)
}

// Remove deletes the named file or (empty) directory.
func (fsys *WinFsys) Remove(name string) error {
	if existing, err := fsys.Stat(name); err == nil && existing.IsDir() {
		return fsys.removeDir(name)
	}

	if err := fsys.Exec("del %s", ps.DoubleQuotePath(name)); err != nil {
		return fmt.Errorf("remove %s: %w", name, err)
	}

	return nil
}

// RemoveAll deletes the named file or directory and all its child items
func (fsys *WinFsys) RemoveAll(name string) error {
	if existing, err := fsys.Stat(name); err == nil && existing.IsDir() {
		return fsys.removeDirAll(name)
	}

	return fsys.Remove(name)
}

func (fsys *WinFsys) removeDir(name string) error {
	if err := fsys.Exec("rmdir /q %s", ps.DoubleQuotePath(name)); err != nil {
		return fmt.Errorf("rmdir %s: %w", name, err)
	}
	return nil
}

func (fsys *WinFsys) removeDirAll(name string) error {
	if err := fsys.Exec("rmdir /s /q %s", ps.DoubleQuotePath(name)); err != nil {
		return fmt.Errorf("rmdir %s: %w", name, err)
	}

	return nil
}

// MkDirAll creates a directory named path, along with any necessary parents. The permission bits are ignored on Windows.
func (fsys *WinFsys) MkDirAll(name string, _ fs.FileMode) error {
	if err := fsys.Exec("New-Item -ItemType Directory -Force -Path %s", ps.DoubleQuotePath(name), exec.PS()); err != nil {
		return fmt.Errorf("mkdir %s: %w", name, err)
	}

	return nil
}

type opener interface {
	open(flags int) error
}

// Open opens the named file for reading and returns fs.File.
// Use OpenFile to get a file that can be written to or if you need any of the methods not
// available on fs.File interface without type assertion.
func (fsys *WinFsys) Open(name string) (fs.File, error) {
	f, err := fsys.OpenFile(name, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}

	return f, nil
}

// OpenFile opens the named remote file with the specified flags. os.O_EXCL and permission bits are ignored on Windows.
// For a description of the flags, see https://pkg.go.dev/os#pkg-constants
func (fsys *WinFsys) OpenFile(name string, flags int, _ fs.FileMode) (File, error) {
	name = ps.ToWindowsPath(name)
	fi, err := fsys.Stat(name)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, &fs.PathError{Op: OpOpen, Path: name, Err: fmt.Errorf("stat: %w", err)}
	}
	var o opener
	if fi != nil && fi.IsDir() {
		o = &winDir{winFileDirBase: winFileDirBase{withPath: withPath{name}, fsys: fsys}}
	} else {
		o = &winFile{winFileDirBase: winFileDirBase{withPath: withPath{name}, fsys: fsys}}
	}
	if err := o.open(flags); err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	f, ok := o.(File)
	if !ok {
		return nil, &fs.PathError{Op: OpOpen, Path: name, Err: fmt.Errorf("%w: open: %w", ErrCommandFailed, fs.ErrInvalid)}
	}

	return f, nil
}
