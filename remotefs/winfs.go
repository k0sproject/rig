package remotefs

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/log"
	ps "github.com/k0sproject/rig/v2/powershell"
)

var (
	_ fs.FS = (*WinFS)(nil)
	_ FS    = (*WinFS)(nil)
	// ErrNotSupported is returned when a function is not supported on Windows.
	ErrNotSupported     = errors.New("not supported on windows")
	errScriptError      = errors.New("script error")
	errUnexpectedOutput = errors.New("unexpected output")
)

// WinFS is a fs.FS implemen{.
type WinFS struct {
	cmd.Runner
	log.LoggerInjectable
}

// NewWindowsFS returns a new fs.FS implementing filesystem for Windows targets.
func NewWindowsFS(conn cmd.Runner) *WinFS {
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
	out, err := s.ExecOutput(fmt.Sprintf(statCmdTemplate, ps.DoubleQuotePath(name)), cmd.PS())
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
	script := strings.Join([]string{
		fmt.Sprintf("$in = [System.IO.File]::OpenRead(%s)", ps.DoubleQuotePath(name)),
		"$hash = [System.Security.Cryptography.SHA256]::Create().ComputeHash($in)",
		"$in.Close()",
		`($hash | ForEach-Object { $_.ToString("x2") }) -join ""`,
	}, "; ")
	sum, err := s.ExecOutput("try { "+script+" } catch { exit 1 }", cmd.PS())
	if err != nil {
		return "", PathErrorf("sum", name, "sha256sum: %w", err)
	}
	return strings.TrimSpace(sum), nil
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
	if err := s.Exec("New-Item -ItemType Directory -Force -Path "+ps.DoubleQuotePath(name), cmd.PS()); err != nil {
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
	return s.Exec(fmt.Sprintf("if (Test-Path -LiteralPath %s) { exit 0 } else { exit 1 }", ps.DoubleQuotePath(name)), cmd.PS()) == nil
}

// LookPath checks if a command exists on the host.
func (s *WinFS) LookPath(name string) (string, error) {
	out, err := s.ExecOutput(fmt.Sprintf("Get-Command %s -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1 -ExpandProperty Source", ps.DoubleQuotePath(name)), cmd.PS())
	if err != nil {
		return "", fmt.Errorf("lookpath %s: %w", name, err)
	}
	for line := range strings.SplitSeq(out, "\n") {
		if p := toSlashes(strings.TrimSpace(line)); p != "" {
			return p, nil
		}
	}
	return "", fmt.Errorf("lookpath %s: %w", name, fs.ErrNotExist)
}

// Join joins any number of path elements into a single path, adding a separating slash if necessary.
func (s *WinFS) Join(elem ...string) string {
	return strings.Join(elem, "\\")
}

// winTrimPath strips trailing separators from p and identifies paths that have
// no further components to split, returning them as a ready-to-return value.
// Returns (trimmed, shortCircuit): when shortCircuit is non-empty the caller
// should return it directly without further processing. shortCircuit is set for:
//   - empty input ("") → "."
//   - separator-only input ("\" or "/") → the leading separator character
//   - bare drive root ("C:\") → the drive letter plus its separator ("C:\")
func winTrimPath(p string) (trimmed, shortCircuit string) {
	trimmed = strings.TrimRight(p, "/\\")
	if trimmed == "" {
		if p == "" {
			return "", "."
		}
		return "", string(p[0]) // separator-only path is a root
	}
	// Bare drive letter after stripping a trailing separator ("C:" from "C:\") is a root.
	if len(trimmed) == 2 && trimmed[1] == ':' && len(p) > len(trimmed) {
		return "", trimmed + string(p[len(trimmed)]) // preserve the separator style
	}
	return trimmed, ""
}

// Dir returns all but the last element of path, typically the path's directory.
// Both forward and backward slashes are recognised as separators.
func (s *WinFS) Dir(path string) string {
	trimmed, shortCircuit := winTrimPath(path)
	if shortCircuit != "" {
		return shortCircuit
	}
	idx := strings.LastIndexAny(trimmed, "/\\")
	if idx < 0 {
		return "."
	}
	dir := trimmed[:idx]
	// "C:\foo" or "C:/foo" splits into dir="C:" — restore the root separator.
	if len(dir) == 2 && dir[1] == ':' {
		return dir + string(trimmed[idx])
	}
	if dir == "" {
		return string(trimmed[idx])
	}
	return dir
}

// Base returns the last element of path.
// Both forward and backward slashes are recognised as separators.
func (s *WinFS) Base(path string) string {
	trimmed, shortCircuit := winTrimPath(path)
	if shortCircuit != "" {
		return shortCircuit
	}
	idx := strings.LastIndexAny(trimmed, "/\\")
	if idx < 0 {
		return trimmed
	}
	return trimmed[idx+1:]
}

// CommandExist reports whether the named command is available on the remote host.
func (s *WinFS) CommandExist(name string) bool {
	_, err := s.LookPath(name)
	return err == nil
}

// Touch creates a new file with the given name if it does not exist.
// Without ts, the file's modification time is set to the current time. When ts
// is supplied, the file's modification time is set to the first timestamp provided.
func (s *WinFS) Touch(name string, ts ...time.Time) error {
	value := "(Get-Date).ToUniversalTime()"
	if len(ts) > 0 {
		value = fmt.Sprintf("[DateTime]::Parse(%s).ToUniversalTime()", ps.SingleQuote(ts[0].UTC().Format("2006-01-02T15:04:05.999Z")))
	}
	quotedName := ps.DoubleQuotePath(name)
	script := fmt.Sprintf("if (!(Test-Path -LiteralPath %[1]s)) { [System.IO.File]::Create(%[1]s).Close() }; $f = Get-Item -LiteralPath %[1]s; $f.LastWriteTime = %[2]s", quotedName, value)
	if err := s.Exec(script, cmd.PS()); err != nil {
		return fmt.Errorf("touch %s: %w", name, err)
	}
	return nil
}

// Chtimes changes the access and modification times of the named file, similar to the Unix utime() or utimes() functions.
func (s *WinFS) Chtimes(name string, atime, mtime int64) error {
	atimeMs := time.Unix(0, atime).UTC().Format("2006-01-02T15:04:05.999")
	mtimeMs := time.Unix(0, mtime).UTC().Format("2006-01-02T15:04:05.999")
	if err := s.Exec(fmt.Sprintf("$file = Get-Item %s; $file.LastWriteTime = %s; $file.LastAccessTime = %s", ps.DoubleQuotePath(name), ps.SingleQuote(mtimeMs), ps.SingleQuote(atimeMs)), cmd.PS()); err != nil {
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

// Chown changes the ownership of the named file. On Windows it returns an error.
func (s *WinFS) Chown(name string, _ string) error {
	return fmt.Errorf("chown %s: %w", name, ErrNotSupported)
}

// ChownInt changes the ownership using numeric uid and gid. On Windows it returns an error.
func (s *WinFS) ChownInt(name string, _, _ int) error {
	return fmt.Errorf("chown %s: %w", name, ErrNotSupported)
}

// ChownTree recursively changes the ownership. On Windows it returns an error.
func (s *WinFS) ChownTree(name string, _ string) error {
	return fmt.Errorf("chown -R %s: %w", name, ErrNotSupported)
}

// ChownTreeInt recursively changes the ownership using numeric uid and gid. On Windows it returns an error.
func (s *WinFS) ChownTreeInt(name string, _, _ int) error {
	return fmt.Errorf("chown -R %s: %w", name, ErrNotSupported)
}

// DownloadURL downloads the contents of url to dst using Invoke-WebRequest.
func (s *WinFS) DownloadURL(url, dst string) error {
	script := fmt.Sprintf(`$ProgressPreference='SilentlyContinue'
try {
  Invoke-WebRequest -Uri %s -OutFile %s -UseBasicParsing -ErrorAction Stop | Out-Null
} catch {
  Write-Error $_.Exception.Message
  exit 1
}`, ps.SingleQuote(url), ps.DoubleQuotePath(dst))
	if err := s.Exec(script, cmd.PS()); err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	return nil
}

// RoundTrip implements http.RoundTripper by executing the request via Invoke-WebRequest on the remote host.
func (s *WinFS) RoundTrip(req *http.Request) (*http.Response, error) {
	headerLines := make([]string, 0, len(req.Header))
	for k, vals := range req.Header {
		headerLines = append(headerLines, "  "+ps.SingleQuote(k)+"="+ps.SingleQuote(strings.Join(vals, ", ")))
	}
	headerScript := ""
	if len(headerLines) > 0 {
		headerScript = "$params['Headers']=@{\n" + strings.Join(headerLines, "\n") + "\n}"
	}

	bodyScript := ""
	if req.Body != nil {
		data, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("http round-trip %s: read body: %w", req.URL, err)
		}
		bodyScript = "$params['Body']=[Convert]::FromBase64String(" + ps.SingleQuote(base64.StdEncoding.EncodeToString(data)) + ")"
	}

	script := fmt.Sprintf(`$ProgressPreference='SilentlyContinue'
$ErrorActionPreference='Stop'
function ConvertTo-RawResponse($code,$desc,$hdrs,$body){
  $crlf=[char]13+[char]10
  $head="HTTP/1.1 $code $desc"+$crlf
  $hdrs.GetEnumerator()|ForEach-Object{$head+="$($_.Key): $(@($_.Value) -join ',')" +$crlf}
  $head+=$crlf
  $hb=[Text.Encoding]::ASCII.GetBytes($head)
  $out=[byte[]]::new($hb.Length+$body.Length)
  [Buffer]::BlockCopy($hb,0,$out,0,$hb.Length)
  [Buffer]::BlockCopy($body,0,$out,$hb.Length,$body.Length)
  [Convert]::ToBase64String($out)
}
$params=@{Uri=%s;Method=%s;UseBasicParsing=$true;ErrorAction='Stop'}
%s
%s
try{
  $r=Invoke-WebRequest @params
  $b=if($r.PSObject.Properties['RawContentStream']){$r.RawContentStream.ToArray()}else{[byte[]]$r.Content}
  ConvertTo-RawResponse ([int]$r.StatusCode) $r.StatusDescription $r.Headers $b
}catch [System.Net.WebException]{
  if($_.Exception.Response -ne $null){
    $er=$_.Exception.Response
    $ms=New-Object System.IO.MemoryStream
    $er.GetResponseStream().CopyTo($ms)
    $eh=@{}
    $er.Headers.AllKeys|ForEach-Object{$eh[$_]=$er.Headers[$_]}
    ConvertTo-RawResponse ([int]$er.StatusCode) $er.StatusDescription $eh $ms.ToArray()
  }else{Write-Error $_.Exception.Message;exit 1}
}catch{Write-Error $_.Exception.Message;exit 1}`, ps.SingleQuote(req.URL.String()), ps.SingleQuote(req.Method), headerScript, bodyScript)

	out, err := s.ExecOutputContext(req.Context(), script, cmd.PS())
	if err != nil {
		return nil, fmt.Errorf("http round-trip %s: %w", req.URL, err)
	}
	return parseRawHTTPResponse(out, req)
}

// FileContains reports whether the file at path contains the given substring.
// Returns a not-exist error if the file does not exist.
func (s *WinFS) FileContains(name, substr string) (bool, error) {
	script := fmt.Sprintf(`
$ErrorActionPreference='Stop'
try {
  if (-not (Test-Path -LiteralPath %s)) {
    Write-Output 'NOT_FOUND'
  } elseif (Select-String -Quiet -SimpleMatch -Pattern %s -LiteralPath %s) {
    Write-Output 'MATCH'
  } else {
    Write-Output 'NO_MATCH'
  }
} catch {
  Write-Output ('ERROR:' + $_.Exception.Message)
}`, ps.DoubleQuotePath(name), ps.SingleQuote(substr), ps.DoubleQuotePath(name))
	out, err := s.ExecOutput(script, cmd.PS())
	if err != nil {
		return false, fmt.Errorf("file-contains %s: %w", name, err)
	}
	switch status := strings.TrimSpace(out); {
	case status == "MATCH":
		return true, nil
	case status == "NO_MATCH":
		return false, nil
	case status == "NOT_FOUND":
		return false, PathError("file-contains", name, fs.ErrNotExist)
	case strings.HasPrefix(status, "ERROR:"):
		return false, fmt.Errorf("file-contains %s: %w: %s", name, errScriptError, strings.TrimPrefix(status, "ERROR:"))
	default:
		return false, fmt.Errorf("file-contains %s: %w: %q", name, errUnexpectedOutput, status)
	}
}

// IsContainer reports whether the host is running inside a container.
// Container detection is not supported on Windows.
func (s *WinFS) IsContainer() (bool, error) {
	return false, ErrNotSupported
}

// Truncate changes the size of the named file.
func (s *WinFS) Truncate(name string, size int64) error {
	if err := s.Exec(fmt.Sprintf("Set-Content -Path %s -Value $null -Encoding Byte -Force -NoNewline -Stream '::$DATA' -Offset %d", ps.DoubleQuotePath(name), size), cmd.PS()); err != nil {
		return fmt.Errorf("truncate %s: %w", name, err)
	}
	return nil
}

// Getenv retrieves the value of the environment variable named by the key.
func (s *WinFS) Getenv(key string) string {
	out, err := s.ExecOutput(fmt.Sprintf("[System.Environment]::GetEnvironmentVariable(%s)", ps.SingleQuote(key)), cmd.PS(), cmd.TrimOutput(true))
	if err != nil {
		return ""
	}
	return out
}

// Hostname returns the hostname of the remote host.
func (s *WinFS) Hostname() (string, error) {
	out, err := s.ExecOutput("$env:COMPUTERNAME", cmd.PS())
	if err != nil {
		return "", fmt.Errorf("hostname: %w", err)
	}
	return out, nil
}

// MachineID returns the unique machine ID from the Windows registry.
func (s *WinFS) MachineID() (string, error) {
	out, err := s.ExecOutput("(Get-ItemProperty -Path 'HKLM:\\SOFTWARE\\Microsoft\\Cryptography' -Name MachineGuid).MachineGuid", cmd.PS())
	if err != nil {
		return "", fmt.Errorf("machine-id: %w", err)
	}
	if out == "" {
		return "", ErrEmptyMachineID
	}
	return out, nil
}

// SystemTime returns the current UTC time on the remote host.
func (s *WinFS) SystemTime() (time.Time, error) {
	out, err := s.ExecOutput("[DateTimeOffset]::UtcNow.ToUnixTimeSeconds()", cmd.PS())
	if err != nil {
		return time.Time{}, fmt.Errorf("system time: %w", err)
	}
	secs, err := strconv.ParseInt(out, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("system time: parse %q: %w", out, err)
	}
	return time.Unix(secs, 0), nil
}

// LongHostname resolves the FQDN (long) hostname.
func (s *WinFS) LongHostname() (string, error) {
	out, err := s.ExecOutput("([System.Net.Dns]::GetHostByName(($env:COMPUTERNAME))).Hostname", cmd.PS())
	if err != nil {
		return "", fmt.Errorf("hostname (long): %w", err)
	}
	return out, nil
}

// Rename renames (moves) oldpath to newpath.
func (s *WinFS) Rename(oldpath, newpath string) error {
	if err := s.Exec(fmt.Sprintf("Move-Item -Path %s -Destination %s", ps.DoubleQuotePath(oldpath), ps.DoubleQuotePath(newpath)), cmd.PS()); err != nil {
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
