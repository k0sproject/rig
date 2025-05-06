package remotefs

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/log"
	"github.com/k0sproject/rig/v2/sh"
	"github.com/k0sproject/rig/v2/sh/shellescape"
)

var (
	_          fs.FS = (*PosixFS)(nil)
	_          FS    = (*PosixFS)(nil)
	errInvalid       = errors.New("invalid")
	statCmdGNU       = "env -i LC_ALL=C stat -c '%%#f %%s %%.9Y //%%n//' -- %s 2> /dev/null"
	statCmdBSD       = "env -i LC_ALL=C stat -f '%%#p %%z %%Fm //%%N//' -- %s 2> /dev/null"
)

const (
	defaultBlockSize = 4096
	supportedFlags   = os.O_RDONLY | os.O_WRONLY | os.O_RDWR | os.O_CREATE | os.O_EXCL | os.O_TRUNC | os.O_APPEND | os.O_SYNC
)

// PosixFS implements fs.FS for a remote filesystem that uses POSIX commands for access.
type PosixFS struct {
	cmd.Runner
	log.LoggerInjectable

	// TODO: these should probably be in some kind of "coreutils" package
	statCmd   *string
	chtimesFn func(name string, atime, mtime int64) error
	timeTrunc time.Duration
}

// NewPosixFS returns a fs.FS implementation for a remote filesystem that uses POSIX commands for access.
func NewPosixFS(conn cmd.Runner) *PosixFS {
	return &PosixFS{Runner: conn, statCmd: nil, chtimesFn: nil}
}

func (s *PosixFS) initStat() error {
	if s.statCmd != nil {
		return nil
	}
	out, err := s.ExecOutput("stat --help 2>&1", cmd.HideOutput())
	if err != nil {
		return fmt.Errorf("can't access stat command: %w", err)
	}
	if strings.Contains(out, "BusyBox") || strings.Contains(out, "--format=") {
		s.statCmd = &statCmdGNU
		s.timeTrunc = time.Second
	} else {
		s.statCmd = &statCmdBSD
		s.timeTrunc = time.Nanosecond
	}
	return nil
}

// second precision touch for busybox.
func (s *PosixFS) secChtimes(name string, atime, mtime int64) error {
	accessOrMod := [2]rune{'a', 'm'}
	// only supports setting one of them at a time
	for i, t := range [2]int64{atime, mtime} {
		ts := int64ToTime(t)
		utc := ts.UTC()
		cmd := fmt.Sprintf("[ -e %[3]s ] && env -i LC_ALL=C TZ=UTC touch -%[1]c -d @%[2]d -- %[3]s",
			accessOrMod[i],
			utc.Unix(),
			shellescape.Quote(name),
		)
		if err := s.Exec(cmd); err != nil {
			return fmt.Errorf("touch %s (%ctime): %w", name, accessOrMod[i], err)
		}
	}
	return nil
}

// nanosecond precision touch for stats that support it.
func (s *PosixFS) nsecChtimes(name string, atime, mtime int64) error {
	atimeTS := int64ToTime(atime)
	mtimeTS := int64ToTime(mtime)
	utcA := atimeTS.UTC()
	utcM := mtimeTS.UTC()
	cmd := fmt.Sprintf("[ -e %[3]s ] && env -i LC_ALL=C TZ=UTC touch -a -d %[1]s -m -d %[2]s -- %[3]s",
		fmt.Sprintf("%s.%09d", utcA.Format("2006-01-02T15:04:05"), utcA.Nanosecond()),
		fmt.Sprintf("%s.%09d", utcM.Format("2006-01-02T15:04:05"), utcM.Nanosecond()),
		shellescape.Quote(name),
	)
	if err := s.Exec(cmd); err != nil {
		return fmt.Errorf("touch (ns) %s: %w", name, err)
	}
	return nil
}

func (s *PosixFS) initTouch() error {
	if s.chtimesFn != nil {
		return nil
	}
	out, err := s.ExecOutput("touch --help 2>&1", cmd.HideOutput())
	if err != nil {
		return fmt.Errorf("can't access touch command: %w", err)
	}
	if strings.Contains(out, "BusyBox") {
		s.chtimesFn = s.secChtimes
		return nil
	}
	tmpF, err := CreateTemp(s, "", "rigfs-touch-test")
	if err != nil {
		return fmt.Errorf("can't create temp file for touch test: %w", err)
	}
	if err := tmpF.Close(); err != nil {
		return fmt.Errorf("can't close temp file for touch test: %w", err)
	}
	defer func() {
		_ = s.Remove(tmpF.Name())
	}()
	if err := s.nsecChtimes(tmpF.Name(), 0, 0); err != nil {
		s.chtimesFn = s.secChtimes
	} else {
		s.chtimesFn = s.nsecChtimes
	}

	return nil
}

func posixBitsToFileMode(bits int64) fs.FileMode {
	var mode fs.FileMode

	switch bits & 0o170000 {
	case 0o040000: // Directory
		mode |= fs.ModeDir
	case 0o100000: // Regular file
		// nop, no specific FileMode for regular files
	case 0o120000: // Symbolic link
		mode |= fs.ModeSymlink
	case 0o060000: // Block device
		mode |= fs.ModeDevice
	case 0o020000: // Character device
		mode |= fs.ModeDevice | fs.ModeCharDevice
	case 0o010000: // FIFO (Named pipe)
		mode |= fs.ModeNamedPipe
	case 0o140000: // Socket
		mode |= fs.ModeSocket
	}

	// Mapping permission bits
	// Owner, group, and other permissions
	mode |= fs.FileMode(bits & 0o777) // #nosec G115 -- ignore "integer overflow conversion int64 -> uint64"

	// Mapping special permission bits
	if bits&0o4000 != 0 { // Set-user-ID
		mode |= fs.ModeSetuid
	}
	if bits&0o2000 != 0 { // Set-group-ID
		mode |= fs.ModeSetgid
	}
	if bits&0o1000 != 0 { // Sticky bit
		mode |= fs.ModeSticky
	}

	return mode
}

func (s *PosixFS) parseStat(stat string) (*FileInfo, error) {
	// output looks like: 0x81a4 0 1699970097.220228000 //test_20231114155456.txt//
	parts := strings.SplitN(stat, " ", 4)
	if len(parts) != 4 {
		return nil, fmt.Errorf("%w: parse stat output %s", errInvalid, stat)
	}

	res := &FileInfo{fs: s}

	if strings.HasPrefix(parts[0], "0x") {
		m, err := strconv.ParseInt(parts[0][2:], 16, 64)
		if err != nil {
			return nil, fmt.Errorf("parse stat mode %s: %w", stat, err)
		}
		res.FMode = posixBitsToFileMode(m)
	} else {
		m, err := strconv.ParseInt(parts[0], 8, 64)
		if err != nil {
			return nil, fmt.Errorf("parse stat mode %s: %w", stat, err)
		}
		res.FMode = posixBitsToFileMode(m)
	}

	res.FIsDir = res.FMode&fs.ModeDir != 0

	size, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse stat size %s: %w", stat, err)
	}
	res.FSize = size

	timeParts := strings.SplitN(parts[2], ".", 2)
	mtime, err := strconv.ParseInt(timeParts[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse stat mtime %s: %w", stat, err)
	}
	var mtimeNano int64
	if len(timeParts) == 2 {
		mtimeNano, err = strconv.ParseInt(timeParts[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse stat mtime ns %s: %w", stat, err)
		}
	}
	res.FModTime = time.Unix(mtime, mtimeNano)
	res.FName = strings.TrimSuffix(strings.TrimPrefix(parts[3], "//"), "//")

	return res, nil
}

func (s *PosixFS) multiStat(names ...string) ([]fs.FileInfo, error) { //nolint:cyclop // TODO refactor
	if err := s.initStat(); err != nil {
		return nil, err
	}
	var idx int
	res := make([]fs.FileInfo, 0, len(names))
	var batch strings.Builder
	batch.Grow(1024)
	for idx < len(names) {
		batch.Reset()
		// build max 1kb batches of names to stat
		for batch.Len() < 1024 && idx < len(names) {
			if names[idx] != "" {
				batch.WriteString(shellescape.Quote(names[idx]))
				if idx < len(names)-1 {
					batch.WriteRune(' ')
				}
			}
			idx++
		}

		scanner := s.ExecScanner(fmt.Sprintf(*s.statCmd, batch.String()))
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			info, err := s.parseStat(line)
			if err != nil {
				return res, err
			}
			res = append(res, info)
		}
		if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
			if len(names) == 1 {
				return nil, PathError(OpStat, names[0], fs.ErrNotExist)
			}
			return res, fmt.Errorf("stat %s: %w", names, err)
		}
	}
	return res, nil
}

// Stat returns the FileInfo structure describing file.
func (s *PosixFS) Stat(name string) (fs.FileInfo, error) {
	items, err := s.multiStat(name)
	if err != nil {
		return nil, err
	}
	switch len(items) {
	case 0:
		return nil, PathError(OpStat, name, fs.ErrNotExist)
	case 1:
		return items[0], nil
	default:
		return nil, fmt.Errorf("%w: stat %s: too many results", errInvalid, name)
	}
}

// Sha256 returns the sha256 checksum of the file at path.
func (s *PosixFS) Sha256(name string) (string, error) {
	out, err := s.ExecOutput(sh.Command("sha256sum", "-b", name))
	if err != nil {
		if isNotExist(err) {
			return "", PathError("sha256sum", name, fs.ErrNotExist)
		}
		return "", fmt.Errorf("sha256sum %s: %w", name, err)
	}
	sha := strings.Fields(out)[0]
	if len(sha) != 64 {
		return "", fmt.Errorf("%w: sha256sum invalid output %s: %s", errInvalid, name, out)
	}
	return sha, nil
}

// Touch creates a new empty file at path or updates the timestamp of an existing file to the current time.
func (s *PosixFS) Touch(name string) error {
	err := s.Exec(sh.Command("touch", "--", name))
	if err != nil {
		return fmt.Errorf("touch %s: %w", name, err)
	}
	return nil
}

func int64ToTime(timestamp int64) time.Time {
	seconds := timestamp / 1e9
	nanoseconds := timestamp % 1e9
	return time.Unix(seconds, nanoseconds)
}

// Chtimes changes the access and modification times of the named file.
func (s *PosixFS) Chtimes(name string, atime, mtime int64) error {
	if err := s.initTouch(); err != nil {
		return err
	}
	return s.chtimesFn(name, atime, mtime)
}

// Truncate changes the size of the named file or creates a new file if it doesn't exist.
func (s *PosixFS) Truncate(name string, size int64) error {
	if err := s.Exec(sh.Command("truncate", "-s", strconv.FormatInt(size, 10), name)); err != nil {
		return fmt.Errorf("truncate %s: %w", name, err)
	}
	return nil
}

// Chmod changes the mode of the named file to mode.
func (s *PosixFS) Chmod(name string, mode fs.FileMode) error {
	if err := s.Exec(fmt.Sprintf("chmod %#o %s", mode, shellescape.Quote(name))); err != nil {
		if isNotExist(err) {
			return PathError("chmod", name, fs.ErrNotExist)
		}
		return fmt.Errorf("chmod %s: %w", name, err)
	}
	return nil
}

// Chown changes the numeric uid and gid of the named file.
func (s *PosixFS) Chown(name string, uid, gid int) error {
	if err := s.Exec(fmt.Sprintf("chown %d:%d %s", uid, gid, shellescape.Quote(name))); err != nil {
		if isNotExist(err) {
			return PathError("chown", name, fs.ErrNotExist)
		}
		return fmt.Errorf("chown %s: %w", name, err)
	}
	return nil
}

// Open opens the named file for reading.
func (s *PosixFS) Open(name string) (fs.File, error) {
	return s.OpenFile(name, os.O_RDONLY, 0)
}

func (s *PosixFS) openNew(name string, flags int, perm fs.FileMode) (fs.FileInfo, error) {
	if flags&os.O_CREATE == 0 {
		return nil, PathError(OpOpen, name, fs.ErrNotExist)
	}

	if _, err := s.Stat(path.Dir(name)); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, PathErrorf(OpOpen, name, "%w: parent directory does not exist", fs.ErrNotExist)
		}
		return nil, PathErrorf(OpOpen, name, "%w: failed to stat parent directory", fs.ErrInvalid)
	}

	if err := s.Exec(fmt.Sprintf("install -m %#o /dev/null %s", perm, shellescape.Quote(name))); err != nil {
		return nil, PathError(OpOpen, name, err)
	}

	// re-stat to ensure file is now there and get the correct bits if there's a umask
	return s.Stat(name)
}

func (s *PosixFS) openExisting(name string, flags int, info fs.FileInfo) (fs.FileInfo, error) {
	// directories can't be opened for writing
	if info.IsDir() && flags&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_EXCL) != 0 {
		return nil, PathErrorf(OpOpen, name, "%w: is a directory", fs.ErrInvalid)
	}

	// if O_CREATE and O_EXCL are set, the file must not exist
	if flags&(os.O_CREATE|os.O_EXCL) == (os.O_CREATE | os.O_EXCL) {
		return nil, PathError(OpOpen, name, fs.ErrExist)
	}

	if flags&os.O_TRUNC != 0 {
		if err := s.Truncate(name, 0); err != nil {
			return nil, err
		}
	}

	return s.Stat(name)
}

// OpenFile is used to open a file with access/creation flags for reading or writing. For info on flags,
// see https://pkg.go.dev/os#pkg-constants
func (s *PosixFS) OpenFile(name string, flags int, perm fs.FileMode) (File, error) {
	if flags&^supportedFlags != 0 {
		return nil, fmt.Errorf("%w: unsupported flags: %d", errInvalid, flags)
	}

	info, err := s.Stat(name)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		info, err = s.openNew(name, flags, perm)
	} else {
		info, err = s.openExisting(name, flags, info)
	}

	if err != nil {
		return nil, err
	}

	var pos int64
	if flags&os.O_APPEND != 0 {
		pos = info.Size()
	}

	file := &PosixFile{
		withPath: withPath{name},
		fs:       s,
		isOpen:   true,
		size:     info.Size(),
		pos:      pos,
		mode:     info.Mode(),
		flags:    flags,
	}
	if info.IsDir() {
		return &PosixDir{PosixFile: *file}, nil
	}
	return file, nil
}

func scanNullTerminatedStrings(data []byte, atEOF bool) (advance int, token []byte, err error) { //nolint:nonamedreturns // clarity
	if idx := bytes.IndexByte(data, '\x00'); idx >= 0 {
		return idx + 1, data[:idx], nil
	}

	if atEOF && len(data) > 0 {
		return len(data), data, bufio.ErrFinalToken
	}

	return 0, nil, nil
}

// ReadDir reads the directory named by dirname and returns a list of directory entries.
func (s *PosixFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if name == "" {
		name = "."
	}

	scanner := s.ExecScanner(sh.Command("find", name, "-maxdepth", "1", "-print0"))
	scanner.Split(scanNullTerminatedStrings)

	var items []string
	for scanner.Scan() {
		items = append(items, scanner.Text())
	}

	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read dir (find) %s: %w", name, err)
	}

	if len(items) == 0 || (len(items) == 1 && items[0] == "") {
		return nil, PathError("read dir", name, fs.ErrNotExist)
	}
	if items[0] != name {
		return nil, PathError("read dir", name, fs.ErrNotExist)
	}
	if len(items) == 1 {
		return nil, nil
	}

	res := make([]fs.DirEntry, 0, len(items)-1)
	infos, err := s.multiStat(items[1:]...)
	for _, entry := range infos {
		if info, ok := entry.(fs.DirEntry); ok {
			res = append(res, info)
		}
	}
	return res, err
}

// Remove deletes the named file or (empty) directory.
func (s *PosixFS) Remove(name string) error {
	if err := s.Exec(sh.Command("rm", "-f", name)); err != nil {
		return fmt.Errorf("delete %s: %w", name, err)
	}
	return nil
}

func isNotExist(err error) bool {
	return err != nil && (errors.Is(err, fs.ErrNotExist) || strings.Contains(err.Error(), "No such file or directory"))
}

// RemoveAll removes path and any children it contains.
func (s *PosixFS) RemoveAll(name string) error {
	if err := s.Exec(sh.Command("rm", "-rf", name)); err != nil {
		return fmt.Errorf("remove all %s: %w", name, err)
	}
	return nil
}

// Rename renames (moves) oldpath to newpath.
func (s *PosixFS) Rename(oldpath, newpath string) error {
	if err := s.Exec(sh.Command("mv", "-f", oldpath, newpath)); err != nil {
		return fmt.Errorf("rename %s -> %s: %w", oldpath, newpath, err)
	}
	return nil
}

// TempDir returns the default directory to use for temporary files.
func (s *PosixFS) TempDir() string {
	out, err := s.ExecOutput("echo ${TMPDIR:-/tmp}")
	if err != nil {
		return "/tmp"
	}
	return out
}

// MkdirAll creates a new directory structure with the specified name and permission bits.
// If the directory already exists, MkDirAll does nothing and returns nil.
func (s *PosixFS) MkdirAll(name string, perm fs.FileMode) error {
	dir := shellescape.Quote(name)
	if existing, err := s.Stat(name); err == nil {
		if existing.IsDir() {
			return nil
		}
		return fmt.Errorf("mkdir %s: %w", name, fs.ErrExist)
	}

	if err := s.Exec(fmt.Sprintf("install -d -m %#o %s", perm, shellescape.Quote(dir))); err != nil {
		return fmt.Errorf("mkdir %s: %w", name, err)
	}

	return nil
}

// Mkdir creates a new directory with the specified name and permission bits.
func (s *PosixFS) Mkdir(name string, perm fs.FileMode) error {
	if err := s.Exec(fmt.Sprintf("mkdir -m %#o %s", perm, shellescape.Quote(name))); err != nil {
		return PathError("mkdir", name, err)
	}

	return nil
}

// WriteFile writes data to a file named by filename.
func (s *PosixFS) WriteFile(filename string, data []byte, perm fs.FileMode) error {
	if err := s.Exec(fmt.Sprintf("install -D -m %#o /dev/stdin %s", perm, shellescape.Quote(filename)), cmd.Stdin(bytes.NewReader(data))); err != nil {
		return fmt.Errorf("write file %s: %w", filename, err)
	}
	return nil
}

// ReadFile reads the file named by filename and returns the contents.
func (s *PosixFS) ReadFile(filename string) ([]byte, error) {
	out, err := s.ExecOutput(sh.Command("cat", "--", filename), cmd.HideOutput(), cmd.TrimOutput(false))
	if err != nil {
		return nil, fmt.Errorf("read file %s: %w", filename, err)
	}
	return []byte(out), nil
}

// MkdirTemp creates a new temporary directory in the directory dir with a name beginning with prefix and returns the path of the new directory.
func (s *PosixFS) MkdirTemp(dir, prefix string) (string, error) {
	if dir == "" {
		dir = s.TempDir()
	}
	out, err := s.ExecOutput(sh.Command("mktemp", "-d", s.Join(dir, prefix+"XXXXXX")))
	if err != nil {
		return "", fmt.Errorf("mkdir temp %s: %w", dir, err)
	}
	return out, nil
}

// FileExist checks if a file exists on the host.
func (s *PosixFS) FileExist(name string) bool {
	return s.Exec(sh.Command("test", "-f", name), cmd.HideOutput()) == nil
}

// LookPath checks if a command exists on the host.
func (s *PosixFS) LookPath(name string) (string, error) {
	path, err := s.ExecOutput(sh.Command("command", "-v", name), cmd.HideOutput())
	if err != nil {
		return "", fmt.Errorf("lookpath %s: %w", name, err)
	}
	return path, nil
}

// Join joins any number of path elements into a single path, adding a separating slash if necessary.
func (s *PosixFS) Join(elem ...string) string {
	return path.Join(elem...)
}

// Getenv returns the value of the environment variable named by the key.
func (s *PosixFS) Getenv(key string) string {
	out, err := s.ExecOutput(fmt.Sprintf("echo ${%s}", key), cmd.HideOutput())
	if err != nil {
		return ""
	}
	return out
}

// Hostname returns the name of the host.
func (s *PosixFS) Hostname() (string, error) {
	out, err := s.ExecOutput("hostname")
	if err != nil {
		return "", fmt.Errorf("hostname: %w", err)
	}
	return out, nil
}

// LongHostname returns the FQDN of the host.
func (s *PosixFS) LongHostname() (string, error) {
	out, err := s.ExecOutput("hostname -f 2> /dev/null")
	if err != nil {
		return "", fmt.Errorf("hostname -f: %w", err)
	}

	return out, nil
}

// UserCacheDir returns the default root directory to use for user-specific cached data.
func (s *PosixFS) UserCacheDir() string {
	if cache := s.Getenv("XDG_CACHE_HOME"); cache != "" {
		return cache
	}
	return s.Join(s.UserHomeDir(), ".cache")
}

// UserConfigDir returns the default root directory to use for user-specific configuration data.
func (s *PosixFS) UserConfigDir() string {
	if config := s.Getenv("XDG_CONFIG_HOME"); config != "" {
		return config
	}
	return s.Join(s.UserHomeDir(), ".config")
}

// UserHomeDir returns the current user's home directory.
func (s *PosixFS) UserHomeDir() string {
	return s.Getenv("HOME")
}
