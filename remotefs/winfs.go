package remotefs

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/log"
	ps "github.com/k0sproject/rig/powershell"
)

var (
	_ fs.FS = (*WinFS)(nil)
	_ FS    = (*WinFS)(nil)
	// EWINDOWS is returned when a function is not supported on Windows.
	EWINDOWS = errors.New("not supported on windows") //nolint:revive,stylecheck // modeled after syscall
)

// WinFS is a fs.FS implemen{.
type WinFS struct {
	exec.Runner
	log.LoggerInjectable
}

// NewWindowsFS returns a new fs.FS implementing filesystem for Windows targets.
func NewWindowsFS(conn exec.Runner) *WinFS {
	return &WinFS{Runner: conn}
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
func (s *WinFS) Stat(name string) (fs.FileInfo, error) {
	out, err := s.ExecOutput(fmt.Sprintf(statCmdTemplate, ps.DoubleQuotePath(name)), exec.PS())
	if err != nil {
		return nil, PathErrorf(OpStat, name, "%w: %w", err, fs.ErrNotExist)
	}

	fi := &winFileInfo{fs: s}
	if err := json.Unmarshal([]byte(out), fi); err != nil {
		return nil, PathErrorf(OpStat, name, "%w: stat (parse)", err)
	}
	if fi.Err != "" {
		if strings.Contains(fi.Err, "does not exist") {
			return nil, PathError(OpStat, name, fs.ErrNotExist)
		}
		return nil, PathErrorf(OpStat, name, "stat: %v", fi.Err)
	}
	return fi, nil
}

// Sha256 returns the SHA256 hash of the remote file.
func (s *WinFS) Sha256(name string) (string, error) {
	sum, err := s.ExecOutput(fmt.Sprintf("(Get-FileHash %s -Algorithm SHA256).Hash.ToLower()", ps.DoubleQuotePath(name)), exec.PS())
	if err != nil {
		return "", PathErrorf("sum", name, "sha256sum: %w", err)
	}
	return sum, nil
}

// ReadDir reads the directory named by dirname and returns a list of directory entries.
func (s *WinFS) ReadDir(name string) ([]fs.DirEntry, error) {
	f, err := s.OpenFile(name, os.O_RDONLY, 0)
	if err != nil {
		return nil, PathError("readdir", name, err)
	}
	defer f.Close()
	dir, ok := f.(*winDir)
	if !ok {
		return nil, PathErrorf("readdir", name, "readdir: %w", fs.ErrInvalid)
	}

	return dir.ReadDir(-1)
}

// Remove deletes the named file or (empty) directory.
func (s *WinFS) Remove(name string) error {
	if existing, err := s.Stat(name); err == nil && existing.IsDir() {
		return s.removeDir(name)
	}

	if err := s.Exec("cmd.exe /c del " + ps.DoubleQuotePath(name)); err != nil {
		return fmt.Errorf("remove %s: %w", name, err)
	}

	return nil
}

// RemoveAll deletes the named file or directory and all its child items.
func (s *WinFS) RemoveAll(name string) error {
	if existing, err := s.Stat(name); err == nil && existing.IsDir() {
		return s.removeDirAll(name)
	}

	return s.Remove(name)
}

func (s *WinFS) removeDir(name string) error {
	if err := s.Exec("cmd.exe /c rmdir /q " + ps.DoubleQuotePath(name)); err != nil {
		return fmt.Errorf("rmdir %s: %w", name, err)
	}
	return nil
}

func (s *WinFS) removeDirAll(name string) error {
	if err := s.Exec("cmd.exe /c rmdir /s /q " + ps.DoubleQuotePath(name)); err != nil {
		return fmt.Errorf("rmdir %s: %w", name, err)
	}

	return nil
}

// MkdirAll creates a directory named path, along with any necessary parents. The permission bits are ignored on Windows.
func (s *WinFS) MkdirAll(name string, _ fs.FileMode) error {
	if err := s.Exec("New-Item -ItemType Directory -Force -Path "+ps.DoubleQuotePath(name), exec.PS()); err != nil {
		return fmt.Errorf("mkdir %s: %w", name, err)
	}

	return nil
}

// Mkdir creates a new directory with the specified name and permission bits. The permission bits are ignored on Windows.
func (s *WinFS) Mkdir(name string, _ fs.FileMode) error {
	if err := s.Exec("cmd.exe /c mkdir " + ps.DoubleQuotePath(name)); err != nil {
		return PathError("mkdir", name, err)
	}
	return nil
}

func randHexString(n int) (string, error) {
	bytes := make([]byte, (n+1)/2) // generate enough bytes
	_, err := rand.Read(bytes)
	if err != nil {
		return "", fmt.Errorf("create random string: %w", err)
	}
	return hex.EncodeToString(bytes)[:n], nil
}

// MkdirTemp creates a new temporary directory in the directory dir with a name beginning with prefix and returns the path of the new directory.
func (s *WinFS) MkdirTemp(dir, prefix string) (string, error) {
	if dir == "" {
		dir = s.TempDir()
	}

	rnd, err := randHexString(8)
	if err != nil {
		rnd = strconv.FormatInt(time.Now().UnixNano(), 16)
	}

	path := s.Join(dir, prefix+rnd+".tmp")
	if err := s.Mkdir(path, 0o700); err != nil {
		return "", fmt.Errorf("mkdirtemp %s: %w", path, err)
	}
	return toSlashes(path), nil
}

type opener interface {
	open(flags int) error
}

// Open opens the named file for reading and returns fs.File.
// Use OpenFile to get a file that can be written to or if you need any of the methods not
// available on fs.File interface without type assertion.
func (s *WinFS) Open(name string) (fs.File, error) {
	f, err := s.OpenFile(name, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}

	return f, nil
}

// OpenFile opens the named remote file with the specified flags. os.O_EXCL and permission bits are ignored on Windows.
// For a description of the flags, see https://pkg.go.dev/os#pkg-constants
func (s *WinFS) OpenFile(name string, flags int, _ fs.FileMode) (File, error) {
	name = ps.ToWindowsPath(name)
	fi, err := s.Stat(name)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, PathErrorf(OpOpen, name, "stat: %w", err)
	}
	var o opener
	if fi != nil && fi.IsDir() {
		o = &winDir{winFileDirBase: winFileDirBase{withPath: withPath{name}, fs: s}}
	} else {
		o = &winFile{winFileDirBase: winFileDirBase{withPath: withPath{name}, fs: s}}
	}
	if err := o.open(flags); err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	f, ok := o.(File)
	if !ok {
		return nil, PathErrorf(OpOpen, name, "open: %w", fs.ErrInvalid)
	}

	return f, nil
}

// ReadFile reads the named file and returns its contents.
func (s *WinFS) ReadFile(name string) ([]byte, error) {
	f, err := s.Open(name)
	if err != nil {
		return nil, fmt.Errorf("readfile %s: %w", name, err)
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("readfile %s: %w", name, err)
	}
	return data, nil
}

// WriteFile writes data to the named file, creating it if necessary.
func (s *WinFS) WriteFile(name string, data []byte, mode fs.FileMode) error {
	f, err := s.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("writefile %s: %w", name, err)
	}
	defer f.Close()
	reader := bytes.NewReader(data)
	_, err = io.Copy(f, reader)
	if err != nil {
		return fmt.Errorf("writefile copy data %s: %w", name, err)
	}
	return nil
}

// FileExist checks if a file exists on the host. It is a more efficient shortcut for something like:
//
//		if _, err := fs.Stat(name); os.IsNotExist(err) { ... }
//	 if !fs.FileExist(name) { ... }
func (s *WinFS) FileExist(name string) bool {
	return s.Exec(fmt.Sprintf("if (Test-Path -LiteralPath %s) { exit 0 } else { exit 1 }", ps.DoubleQuotePath(name)), exec.PS()) == nil
}

// LookPath checks if a command exists on the host.
func (s *WinFS) LookPath(name string) (string, error) {
	path, err := s.ExecOutput(fmt.Sprintf("Get-Command %s -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Source", ps.DoubleQuotePath(name)), exec.PS())
	if err != nil {
		return "", fmt.Errorf("lookpath %s: %w", name, err)
	}
	return toSlashes(path), nil
}

// Join joins any number of path elements into a single path, adding a separating slash if necessary.
func (s *WinFS) Join(elem ...string) string {
	return strings.Join(elem, "\\")
}

// Touch creates a new file with the given name if it does not exist.
// If the file exists, the access and modification times are set to the current time.
func (s *WinFS) Touch(name string) error {
	if err := s.Exec(fmt.Sprintf("Get-Item %[1]s -ErrorAction SilentlyContinue | Set-ItemProperty -Name LastWriteTime -Value (Get-Date); if (!$?) { New-Item %[1]s -ItemType File }", name), exec.PS()); err != nil {
		return fmt.Errorf("touch %s: %w", name, err)
	}
	return nil
}

// Chtimes changes the access and modification times of the named file, similar to the Unix utime() or utimes() functions.
func (s *WinFS) Chtimes(name string, atime, mtime int64) error {
	atimeMs := time.Unix(0, atime).UTC().Format("2006-01-02T15:04:05.999")
	mtimeMs := time.Unix(0, mtime).UTC().Format("2006-01-02T15:04:05.999")
	if err := s.Exec(fmt.Sprintf("$file = Get-Item %s; $file.LastWriteTime = %s; $file.LastAccessTime = %s", ps.DoubleQuotePath(name), ps.SingleQuote(mtimeMs), ps.SingleQuote(atimeMs)), exec.PS()); err != nil {
		return fmt.Errorf("chtimes %s: %w", name, err)
	}
	return nil
}

// Chmod changes the mode of the named file to mode. On Windows, only the 0200 bit (owner writable) of mode is used; it controls whether the file's read-only attribute is set or cleared.
func (s *WinFS) Chmod(name string, mode fs.FileMode) error {
	var attribSign string
	if mode&0o200 != 0 {
		attribSign = "+"
	} else {
		attribSign = "-"
	}
	if err := s.Exec(fmt.Sprintf("attrib %sR %s", attribSign, ps.DoubleQuotePath(name))); err != nil {
		return fmt.Errorf("chmod %s: %w", name, err)
	}
	return nil
}

// Chown changes the numeric uid and gid of the named file. On windows it returns an error.
func (s *WinFS) Chown(name string, _, _ int) error {
	return fmt.Errorf("chown %s: %w", name, EWINDOWS)
}

// Truncate changes the size of the named file.
func (s *WinFS) Truncate(name string, size int64) error {
	if err := s.Exec(fmt.Sprintf("Set-Content -Path %s -Value $null -Encoding Byte -Force -NoNewline -Stream '::$DATA' -Offset %d", ps.DoubleQuotePath(name), size), exec.PS()); err != nil {
		return fmt.Errorf("truncate %s: %w", name, err)
	}
	return nil
}

// Getenv retrieves the value of the environment variable named by the key.
func (s *WinFS) Getenv(key string) string {
	out, err := s.ExecOutput(fmt.Sprintf("[System.Environment]::GetEnvironmentVariable(%s)", ps.SingleQuote(key)), exec.PS(), exec.TrimOutput(true))
	if err != nil {
		return ""
	}
	return out
}

// Hostname returns the hostname of the remote host.
func (s *WinFS) Hostname() (string, error) {
	out, err := s.ExecOutput("$env:COMPUTERNAME", exec.PS())
	if err != nil {
		return "", fmt.Errorf("hostname: %w", err)
	}
	return out, nil
}

// LongHostname resolves the FQDN (long) hostname.
func (s *WinFS) LongHostname() (string, error) {
	out, err := s.ExecOutput("([System.Net.Dns]::GetHostByName(($env:COMPUTERNAME))).Hostname", exec.PS())
	if err != nil {
		return "", fmt.Errorf("hostname (long): %w", err)
	}
	return out, nil
}

// Rename renames (moves) oldpath to newpath.
func (s *WinFS) Rename(oldpath, newpath string) error {
	if err := s.Exec(fmt.Sprintf("Move-Item -Path %s -Destination %s", ps.DoubleQuotePath(oldpath), ps.DoubleQuotePath(newpath)), exec.PS()); err != nil {
		return fmt.Errorf("rename %s: %w", oldpath, err)
	}
	return nil
}

// TempDir returns the default directory to use for temporary files.
func (s *WinFS) TempDir() string {
	if dir := s.Getenv("TEMP"); dir != "" {
		return toSlashes(dir)
	}
	return "C:/Windows/Temp"
}

// UserCacheDir returns the default root directory to use for user-specific non-essential data files.
func (s *WinFS) UserCacheDir() string {
	if dir := s.Getenv("LOCALAPPDATA"); dir != "" {
		return toSlashes(dir)
	}
	return fmt.Sprintf("C:/Users/%s/AppData/Local", s.Getenv("USERNAME"))
}

// UserConfigDir returns the default root directory to use for user-specific configuration data.
func (s *WinFS) UserConfigDir() string {
	if dir := s.Getenv("APPDATA"); dir != "" {
		return toSlashes(dir)
	}
	return fmt.Sprintf("C:/Users/%s/AppData/Roaming", s.Getenv("USERNAME"))
}

// UserHomeDir returns the current user's home directory.
func (s *WinFS) UserHomeDir() string {
	if dir := s.Getenv("USERPROFILE"); dir != "" {
		return dir
	}
	if user := s.Getenv("USERNAME"); user != "" {
		return "C:/Users/" + user
	}
	return ""
}

func toSlashes(path string) string {
	return strings.ReplaceAll(path, "\\", "/")
}
