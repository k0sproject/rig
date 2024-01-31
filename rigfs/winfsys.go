package rigfs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/k0sproject/rig/exec"
	ps "github.com/k0sproject/rig/powershell"
)

var (
	_        fs.FS = (*WinFsys)(nil)
	_        Fsys  = (*WinFsys)(nil)
	EWINDOWS       = errors.New("not supported on windows")
)

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

// MkdirAll creates a directory named path, along with any necessary parents. The permission bits are ignored on Windows.
func (fsys *WinFsys) MkdirAll(name string, _ fs.FileMode) error {
	if err := fsys.Exec("New-Item -ItemType Directory -Force -Path %s", ps.DoubleQuotePath(name), exec.PS()); err != nil {
		return fmt.Errorf("mkdir %s: %w", name, err)
	}

	return nil
}

// Mkdir creates a new directory with the specified name and permission bits. The permission bits are ignored on Windows.
func (fsys *WinFsys) Mkdir(name string, _ fs.FileMode) error {
	if err := fsys.Exec("mkdir %s", ps.DoubleQuotePath(name)); err != nil {
		return &fs.PathError{Op: "mkdir", Path: name, Err: err}
	}
	return nil
}

// MkdirTemp creates a new temporary directory in the directory dir with a name beginning with prefix and returns the path of the new directory.
func (fsys *WinFsys) MkdirTemp(dir, prefix string) (string, error) {
	if dir == "" {
		dir = fsys.TempDir()
	}

	out, err := fsys.ExecOutput("New-TemporaryFile -Name %s -Path %s", prefix, ps.DoubleQuotePath(dir), exec.PS())
	if err != nil {
		return "", fmt.Errorf("mkdirtemp %s: %w", dir, err)
	}
	return out, nil
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
		return nil, &fs.PathError{Op: OpOpen, Path: name, Err: fmt.Errorf("open: %w", fs.ErrInvalid)}
	}

	return f, nil
}

func (fsys *WinFsys) ReadFile(name string) ([]byte, error) {
	out, err := fsys.ExecOutput("type %s", ps.DoubleQuotePath(name))
	if err != nil {
		return nil, fmt.Errorf("readfile %s: %w", name, err)
	}
	return []byte(out), nil
}

// WriteFile writes data to the named file, creating it if necessary.
func (fsys *WinFsys) WriteFile(name string, data []byte, _ fs.FileMode) error {
	err := fsys.Exec(`$Input | Out-File -FilePath %s`, ps.DoubleQuotePath(name), exec.Stdin(bytes.NewReader(data)), exec.PS())
	if err != nil {
		return fmt.Errorf("writefile %s: %w", name, err)
	}

	return nil
}

// FileExist checks if a file exists on the host
func (fsys *WinFsys) FileExist(name string) bool {
	return fsys.Exec("if (Test-Path -LiteralPath %s) { exit 0 } else { exit 1 }", ps.DoubleQuotePath(name), exec.PS()) == nil
}

// CommandExist checks if a command exists on the host
func (fsys *WinFsys) CommandExist(name string) bool {
	return fsys.Exec("if (Get-Command %s -ErrorAction SilentlyContinue) { exit 0 } else { exit 1 }", name, exec.PS()) == nil
}

// Join joins any number of path elements into a single path, adding a separating slash if necessary.
func (fsys *WinFsys) Join(elem ...string) string {
	return strings.Join(elem, "\\")
}

// Touch creates a new file with the given name if it does not exist.
// If the file exists, the access and modification times are set to the current time.
func (fsys *WinFsys) Touch(name string) error {
	if err := fsys.Exec("Get-Item %[1]s -ErrorAction SilentlyContinue | Set-ItemProperty -Name LastWriteTime -Value (Get-Date); if (!$?) { New-Item %[1]s -ItemType File }", name, exec.PS()); err != nil {
		return fmt.Errorf("touch %s: %w", name, err)
	}
	return nil
}

// Chtimes changes the access and modification times of the named file, similar to the Unix utime() or utimes() functions.
func (fsys *WinFsys) Chtimes(name string, atime, mtime int64) error {
	if err := fsys.Exec("Set-ItemProperty -Path %s -Name LastWriteTime -Value (Get-Date -Date %d)", ps.DoubleQuotePath(name), mtime, exec.PS()); err != nil {
		return fmt.Errorf("chtimes %s: %w", name, err)
	}
	if err := fsys.Exec("Set-ItemProperty -Path %s -Name LastAccessTime -Value (Get-Date -Date %d)", ps.DoubleQuotePath(name), atime, exec.PS()); err != nil {
		return fmt.Errorf("chtimes %s: %w", name, err)
	}
	return nil
}

// Chmod changes the mode of the named file to mode. On Windows, only the 0200 bit (owner writable) of mode is used; it controls whether the file's read-only attribute is set or cleared.
func (fsys *WinFsys) Chmod(name string, mode fs.FileMode) error {
	var attribSign string
	if mode&0200 != 0 {
		attribSign = "+"
	} else {
		attribSign = "-"
	}
	if err := fsys.Exec("attrib %sR %s", attribSign, ps.DoubleQuotePath(name)); err != nil {
		return fmt.Errorf("chmod %s: %w", name, err)
	}
	return nil
}

// Chown changes the numeric uid and gid of the named file. On windows it returns an error.
func (fsys *WinFsys) Chown(name string, uid, gid int) error {
	return fmt.Errorf("chown %s: %w", name, EWINDOWS)
}

// Truncate changes the size of the named file.
func (fsys *WinFsys) Truncate(name string, size int64) error {
	if err := fsys.Exec("Set-Content -Path %s -Value $null -Encoding Byte -Force -NoNewline -Stream '::$DATA' -Offset %d", ps.DoubleQuotePath(name), size, exec.PS()); err != nil {
		return fmt.Errorf("truncate %s: %w", name, err)
	}
	return nil
}

// Getenv retrieves the value of the environment variable named by the key.
func (fsys *WinFsys) Getenv(key string) string {
	out, err := fsys.ExecOutput("[System.Environment]::GetEnvironmentVariable(%s)", ps.SingleQuote(key), exec.PS(), exec.TrimOutput(true))
	if err != nil {
		return ""
	}
	return out
}

// Hostname returns the hostname of the remote host
func (fsys *WinFsys) Hostname() (string, error) {
	out, err := fsys.ExecOutput("$env:COMPUTERNAME", exec.PS())
	if err != nil {
		return "", fmt.Errorf("hostname: %w", err)
	}
	return out, nil
}

// LongHostname resolves the FQDN (long) hostname
func (fsys *WinFsys) LongHostname() (string, error) {
	out, err := fsys.ExecOutput("([System.Net.Dns]::GetHostByName(($env:COMPUTERNAME))).Hostname", exec.PS())
	if err != nil {
		return "", fmt.Errorf("hostname (long): %w", err)
	}
	return out, nil
}

// Rename renames (moves) oldpath to newpath.
func (fsys *WinFsys) Rename(oldpath, newpath string) error {
	if err := fsys.Exec("Move-Item -Path %s -Destination %s", ps.DoubleQuotePath(oldpath), ps.DoubleQuotePath(newpath), exec.PS()); err != nil {
		return fmt.Errorf("rename %s: %w", oldpath, err)
	}
	return nil
}

func cleanWindowsPath(path string) string {
	return strings.ReplaceAll(path, "/", "\\")
}

// TempDir returns the default directory to use for temporary files.
func (fsys *WinFsys) TempDir() string {
	if dir := fsys.Getenv("TEMP"); dir != "" {
		return cleanWindowsPath(dir)
	}
	return "C:/Windows/Temp"
}

// UserCacheDir returns the default root directory to use for user-specific non-essential data files.
func (fsys *WinFsys) UserCacheDir() string {
	if dir := fsys.Getenv("LOCALAPPDATA"); dir != "" {
		return cleanWindowsPath(dir)
	}
	return fmt.Sprintf("C:/Users/%s/AppData/Local", fsys.Getenv("USERNAME"))
}

// UserConfigDir returns the default root directory to use for user-specific configuration data.
func (fsys *WinFsys) UserConfigDir() string {
	if dir := fsys.Getenv("APPDATA"); dir != "" {
		return cleanWindowsPath(dir)
	}
	return fmt.Sprintf("C:/Users/%s/AppData/Roaming", fsys.Getenv("USERNAME"))
}

// UserHomeDir returns the current user's home directory.
func (fsys *WinFsys) UserHomeDir() string {
	if dir := fsys.Getenv("USERPROFILE"); dir != "" {
		return dir
	}
	if user := fsys.Getenv("USERNAME"); user != "" {
		return fmt.Sprintf("C://Users/%s", user)
	}
	return ""
}
