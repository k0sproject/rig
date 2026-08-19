package remotefs

import (
	"bufio"
	"bytes"
	"context"
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
	_                    fs.FS = (*PosixFS)(nil)
	_                    FS    = (*PosixFS)(nil)
	errInvalid                 = errors.New("invalid")
	errNoDownloadTool          = errors.New("neither curl nor wget is available on the remote host")
	errWgetStatusUnknown       = errors.New("could not determine http status from wget output")
	errGrepFailed              = errors.New("grep failed")
	errTestFailed              = errors.New("test failed")
	errStatInitFailed          = errors.New("stat command not found or unsupported stat implementation")

	// The modification time is read from %y, which spells the timestamp out, rather
	// than from the epoch seconds of %.9Y: the uutils (Rust) reimplementation of
	// coreutils, the default on Ubuntu 25.10 and later, formats %.9Y through a float
	// and truncates the outcome onto a 100ns grid, leaving the time it reports up to
	// ~200ns away from the one the file actually has. %y is exact there, GNU coreutils
	// prints the same layout, and busybox, which ignores the precision of %.9Y
	// altogether, does too.
	statCmdGNU = `env -i PATH="$PATH" LC_ALL=C stat -c '%%#f %%s %%y //%%n//' -- %s 2> /dev/null`
	statCmdBSD = `env -i PATH="$PATH" LC_ALL=C stat -f '%%#p %%z %%Fm //%%N//' -- %s 2> /dev/null`
)

const (
	defaultBlockSize = 4096
	supportedFlags   = os.O_RDONLY | os.O_WRONLY | os.O_RDWR | os.O_CREATE | os.O_EXCL | os.O_TRUNC | os.O_APPEND | os.O_SYNC

	// statTimeLayout is the timestamp format of stat -c %y under LC_ALL=C: a date, a
	// time with a fraction of a second and a UTC offset.
	statTimeLayout = "2006-01-02 15:04:05.999999999 -0700"
	// statDateLayout is the date the %y timestamp starts with, its first field.
	statDateLayout = "2006-01-02"
)

// PosixFS implements fs.FS for a remote filesystem that uses POSIX commands for access.
type PosixFS struct {
	cmd.Runner
	log.LoggerInjectable

	// TODO: these should probably be in some kind of "coreutils" package
	statCmd   *string
	chtimesFn func(name string, atime, mtime int64) error
}

// NewPosixFS returns a fs.FS implementation for a remote filesystem that uses POSIX commands for access.
func NewPosixFS(conn cmd.Runner) *PosixFS {
	return &PosixFS{Runner: conn, statCmd: nil, chtimesFn: nil}
}

func (s *PosixFS) initStat() error {
	if s.statCmd != nil {
		return nil
	}

	if err := s.Exec("stat -c %n /"); err == nil {
		s.statCmd = &statCmdGNU
		return nil
	}

	if err := s.Exec("stat -s /"); err == nil {
		s.statCmd = &statCmdBSD
		return nil
	}

	return errStatInitFailed
}

// chtimes sets the access and modification times of name from timestamps in the
// -d format of touch, the access time first.
//
// The times are set in separate invocations because -d is a single valued option:
// uutils coreutils rejects a repeated --date outright ("the argument '--date
// <STRING>' cannot be used multiple times") and GNU touch silently lets the last
// one win, which would set the access time to the modification timestamp.
func (s *PosixFS) chtimes(name string, timestamps [2]string) error {
	accessOrMod := [2]rune{'a', 'm'}
	escapedName := shellescape.Quote(name)
	for i, ts := range timestamps {
		cmd := fmt.Sprintf(`[ -e %[3]s ] && env -i PATH="$PATH" LC_ALL=C TZ=UTC touch -%[1]c -d %[2]s -- %[3]s`,
			accessOrMod[i],
			ts,
			escapedName,
		)
		if err := s.Exec(cmd); err != nil {
			return fmt.Errorf("touch %s (%ctime): %w", name, accessOrMod[i], err)
		}
	}
	return nil
}

// second precision touch for busybox.
func (s *PosixFS) secChtimes(name string, atime, mtime int64) error {
	var timestamps [2]string
	for i, t := range [2]int64{atime, mtime} {
		timestamps[i] = fmt.Sprintf("@%d", int64ToTime(t).UTC().Unix())
	}
	return s.chtimes(name, timestamps)
}

// nanosecond precision touch for stats that support it.
func (s *PosixFS) nsecChtimes(name string, atime, mtime int64) error {
	var timestamps [2]string
	for i, t := range [2]int64{atime, mtime} {
		utc := int64ToTime(t).UTC()
		timestamps[i] = fmt.Sprintf("%s.%09d", utc.Format("2006-01-02T15:04:05"), utc.Nanosecond())
	}
	return s.chtimes(name, timestamps)
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
	defer func() {
		_ = s.Remove(tmpF.Name())
	}()
	if err := tmpF.Close(); err != nil {
		return fmt.Errorf("can't close temp file for touch test: %w", err)
	}
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
	mode |= fs.FileMode(bits & 0o777) // Owner, group, and other permissions

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

// fileModeToPosixBits is the inverse of posixBitsToFileMode for the bits chmod
// understands: the permission bits plus setuid, setgid and sticky. The file
// type bits have no chmod representation and are ignored.
func fileModeToPosixBits(mode fs.FileMode) int64 {
	bits := int64(mode.Perm())

	if mode&fs.ModeSetuid != 0 {
		bits |= 0o4000
	}
	if mode&fs.ModeSetgid != 0 {
		bits |= 0o2000
	}
	if mode&fs.ModeSticky != 0 {
		bits |= 0o1000
	}

	return bits
}

// isStatDate reports whether a stat timestamp field is the date a %y timestamp starts
// with instead of epoch seconds. An epoch can carry a leading minus, but no dash of its own.
func isStatDate(field string) bool {
	return len(field) == len(statDateLayout) && field[4] == '-' && field[7] == '-'
}

// parseStatModTime reads the modification time from the trailing fields of a stat line
// and returns it along with the remainder, which holds the file name.
//
// The %y timestamp of a GNU style stat is spread over three space separated fields -
// date, time and UTC offset - so it reaches into rest, while the %Fm of a BSD stat is a
// single epoch field and leaves rest alone.
func parseStatModTime(field, rest string) (time.Time, string, error) {
	if isStatDate(field) {
		timeParts := strings.SplitN(rest, " ", 3)
		if len(timeParts) != 3 {
			return time.Time{}, "", fmt.Errorf("%w: timestamp is missing its time or offset", errInvalid)
		}
		modTime, err := time.Parse(statTimeLayout, field+" "+timeParts[0]+" "+timeParts[1])
		if err != nil {
			return time.Time{}, "", fmt.Errorf("parse timestamp: %w", err)
		}
		return modTime, timeParts[2], nil
	}

	epochParts := strings.SplitN(field, ".", 2)
	seconds, err := strconv.ParseInt(epochParts[0], 10, 64)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("parse epoch seconds: %w", err)
	}
	var nanoseconds int64
	if len(epochParts) == 2 {
		nanoseconds, err = strconv.ParseInt(epochParts[1], 10, 64)
		if err != nil {
			return time.Time{}, "", fmt.Errorf("parse epoch nanoseconds: %w", err)
		}
	}

	return time.Unix(seconds, nanoseconds), rest, nil
}

func (s *PosixFS) parseStat(stat string) (*FileInfo, error) {
	// output looks like: 0x81a4 0 2023-11-14 15:54:56.220228000 +0000 //test.txt//
	// or, from a BSD stat: 0x81a4 0 1699970097.220228000 //test.txt//
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

	modTime, name, err := parseStatModTime(parts[2], parts[3])
	if err != nil {
		return nil, fmt.Errorf("parse stat mtime %s: %w", stat, err)
	}
	res.FModTime = modTime
	res.FName = strings.TrimSuffix(strings.TrimPrefix(name, "//"), "//")

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

// Touch creates a new empty file at path or updates the timestamps of an existing file.
// Without ts, both access and modification times are set to the current time. When ts is
// supplied, both times are set to the first timestamp provided.
func (s *PosixFS) Touch(name string, ts ...time.Time) error {
	if err := s.Exec(sh.Command("touch", "--", name)); err != nil {
		return fmt.Errorf("touch %s: %w", name, err)
	}
	if len(ts) > 0 {
		t := ts[0]
		if err := s.Chtimes(name, t.UnixNano(), t.UnixNano()); err != nil {
			return fmt.Errorf("touch %s: %w", name, err)
		}
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

// Chmod changes the mode of the named file to mode. The permission bits and
// the setuid, setgid and sticky bits are applied; the file type bits are
// ignored, as chmod has no representation for them.
func (s *PosixFS) Chmod(name string, mode fs.FileMode) error {
	if err := s.Exec(sh.Command("chmod", fmt.Sprintf("%#o", fileModeToPosixBits(mode)), name)); err != nil {
		if isNotExist(err) {
			return PathError("chmod", name, fs.ErrNotExist)
		}
		return fmt.Errorf("chmod %s: %w", name, err)
	}
	return nil
}

// Chown changes the ownership of the named file. The owner parameter follows
// the standard chown format: "user", "user:group", or ":group".
func (s *PosixFS) Chown(name string, owner string) error {
	if err := s.Exec(sh.Command("chown", "--", owner, name)); err != nil {
		if isNotExist(err) {
			return PathError("chown", name, fs.ErrNotExist)
		}
		return fmt.Errorf("chown %s: %w", name, err)
	}
	return nil
}

// ChownInt changes the ownership of the named file using numeric uid and gid.
func (s *PosixFS) ChownInt(name string, uid, gid int) error {
	owner := fmt.Sprintf("%d:%d", uid, gid)
	if err := s.Exec(sh.Command("chown", "--", owner, name)); err != nil {
		if isNotExist(err) {
			return PathError("chown", name, fs.ErrNotExist)
		}
		return fmt.Errorf("chown %s: %w", name, err)
	}
	return nil
}

// ChownTree recursively changes the ownership of path and all its contents.
// The owner parameter follows the standard chown format: "user", "user:group", or ":group".
func (s *PosixFS) ChownTree(name string, owner string) error {
	if err := s.Exec(sh.Command("chown", "-R", "--", owner, name)); err != nil {
		if isNotExist(err) {
			return PathError("chown -R", name, fs.ErrNotExist)
		}
		return fmt.Errorf("chown -R %s: %w", name, err)
	}
	return nil
}

// ChownTreeInt recursively changes the ownership of path and all its contents using numeric uid and gid.
func (s *PosixFS) ChownTreeInt(name string, uid, gid int) error {
	owner := fmt.Sprintf("%d:%d", uid, gid)
	if err := s.Exec(sh.Command("chown", "-R", "--", owner, name)); err != nil {
		if isNotExist(err) {
			return PathError("chown -R", name, fs.ErrNotExist)
		}
		return fmt.Errorf("chown -R %s: %w", name, err)
	}
	return nil
}

// DownloadURL downloads the contents of url to dst. It prefers curl when available
// and falls back to wget. Returns a descriptive error if neither is available.
func (s *PosixFS) DownloadURL(url, dst string) error {
	if _, err := s.LookPath("curl"); err == nil {
		if err := s.Exec(sh.Command("curl", "-sSLf", "-o", dst, "--", url)); err != nil {
			return fmt.Errorf("download %s: %w", url, err)
		}
		return nil
	}
	if _, err := s.LookPath("wget"); err == nil {
		if err := s.Exec(sh.Command("wget", "-qO", dst, "--", url)); err != nil {
			return fmt.Errorf("download %s: %w", url, err)
		}
		return nil
	}
	return fmt.Errorf("download %s: %w", url, errNoDownloadTool)
}

// httpStatusInsecure checks whether url is reachable and returns the HTTP status
// code. TLS certificate verification is skipped (equivalent to curl -k).
// It prefers curl and falls back to wget.
func (s *PosixFS) httpStatusInsecure(ctx context.Context, rawURL string) (int, error) {
	if _, err := s.LookPath("curl"); err == nil {
		out, err := s.ExecOutputContext(ctx, sh.Command("curl", "-kIso", "/dev/null", "--connect-timeout", "20", "-w", "%{http_code}", "--", rawURL), cmd.Sensitive())
		if err != nil {
			return 0, fmt.Errorf("http-status %s: %w", rawURL, err)
		}
		code, err := strconv.Atoi(strings.TrimSpace(out))
		if err != nil {
			return 0, fmt.Errorf("http-status %s: invalid response %q: %w", rawURL, out, err)
		}
		return code, nil
	}
	if _, err := s.LookPath("wget"); err == nil {
		var errBuf strings.Builder
		execErr := s.ExecContext(ctx, sh.Command("wget", "--server-response", "--spider", "--no-check-certificate", "--max-redirect=0", "-q", "--", rawURL),
			cmd.Sensitive(), cmd.Stderr(&errBuf))
		for line := range strings.SplitSeq(errBuf.String(), "\n") {
			if fields := strings.Fields(line); len(fields) >= 2 && strings.HasPrefix(fields[0], "HTTP/") {
				if code, err := strconv.Atoi(fields[1]); err == nil {
					return code, nil
				}
			}
		}
		if execErr != nil {
			return 0, fmt.Errorf("http-status %s: %w", rawURL, execErr)
		}
		return 0, fmt.Errorf("http-status %s: %w", rawURL, errWgetStatusUnknown)
	}
	return 0, fmt.Errorf("%w: neither curl nor wget found", ErrHTTPStatusNotSupported)
}

// FileContains reports whether the file at path contains the given substring.
// Returns a not-exist error if the file does not exist.
func (s *PosixFS) FileContains(name, substr string) (bool, error) {
	out, err := s.ExecOutput(
		sh.Command("sh", "-c", `grep -qF -- "$1" "$2" >/dev/null 2>&1; printf '%s' "$?"`, "sh", substr, name),
		cmd.HideOutput(),
	)
	if err != nil {
		return false, fmt.Errorf("file-contains %s: %w", name, err)
	}
	status, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return false, fmt.Errorf("file-contains %s: parse grep status %q: %w", name, out, err)
	}
	switch status {
	case 0:
		return true, nil
	case 1:
		return false, nil
	default:
		testOut, testErr := s.ExecOutput(
			sh.Command("sh", "-c", `test -e -- "$1" >/dev/null 2>&1; printf '%s' "$?"`, "sh", name),
			cmd.HideOutput(),
		)
		if testErr != nil {
			return false, fmt.Errorf("file-contains %s: check existence: %w", name, testErr)
		}
		testStatus, err := strconv.Atoi(strings.TrimSpace(testOut))
		if err != nil {
			return false, fmt.Errorf("file-contains %s: parse test status %q: %w", name, testOut, err)
		}
		switch testStatus {
		case 0:
			return false, fmt.Errorf("file-contains %s: %w (exit %d)", name, errGrepFailed, status)
		case 1:
			return false, PathError("file-contains", name, fs.ErrNotExist)
		default:
			return false, fmt.Errorf("file-contains %s: %w (exit %d)", name, errTestFailed, testStatus)
		}
	}
}

// Follow streams new content appended to path to w until ctx is cancelled.
// Cancelling ctx is the expected way to stop following and does not return an error.
func (s *PosixFS) Follow(ctx context.Context, path string, w io.Writer) error {
	err := s.ExecContext(ctx, sh.Command("tail", "-n", "0", "-f", "--", path), cmd.Stdout(w), cmd.HideOutput())
	if err != nil && ctx.Err() != nil {
		return nil //nolint:nilerr // context cancellation is the expected stop signal
	}
	if err != nil {
		return fmt.Errorf("follow %s: %w", path, err)
	}
	return nil
}

// IsContainer reports whether the remote host is running inside a container
// (Docker, Podman, LXC, nspawn, etc.).
func (s *PosixFS) IsContainer() (bool, error) {
	if s.FileExist("/.dockerenv") {
		return true, nil
	}
	if s.FileExist("/run/.containerenv") {
		return true, nil
	}
	out, err := s.ExecOutput(sh.Command("cat", "/proc/1/cgroup"), cmd.HideOutput())
	if err == nil {
		if strings.Contains(out, "docker") || strings.Contains(out, "lxc") || strings.Contains(out, "kubepods") {
			return true, nil
		}
	}
	return false, nil
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

	if err := s.Exec(sh.Command("install", "-m", fmt.Sprintf("%#o", fileModeToPosixBits(perm)), "/dev/null", name)); err != nil {
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
//
// The permission bits of perm are applied to every directory that is created,
// like os.MkdirAll does; directories that already exist are left alone. The
// setgid, setuid and sticky bits of perm are applied to the last directory of
// the path only. The file type bits are ignored.
//
// The mode comes from a umask instead of `install -d -m ...` because the uutils
// (Rust) reimplementation of coreutils, the default on Ubuntu 25.10 and later,
// applies -m only to the last component of the path while GNU install applies it
// to every directory it creates, leaving the intermediate directories at the
// remote default mode. A umask covers all of them, but only the nine permission
// bits, so the special bits need the trailing chmod.
//
// Note that a perm without u+wx makes creating nested directories fail for a
// non-root user, as it does with os.MkdirAll: the intermediate directory can't
// be written to once it has been created.
func (s *PosixFS) MkdirAll(name string, perm fs.FileMode) error {
	if existing, err := s.Stat(name); err == nil {
		if existing.IsDir() {
			return nil
		}
		return fmt.Errorf("mkdir %s: %w", name, fs.ErrExist)
	}

	mode := fileModeToPosixBits(perm)
	hasSpecialBits := mode&^int64(fs.ModePerm) != 0

	command := sh.CommandBuilder(fmt.Sprintf("umask %#o", fs.ModePerm&^perm.Perm())).
		Raw("&&").Raw(sh.Command("mkdir", "-p", "--", name))

	if hasSpecialBits {
		// "--" precedes the mode because BSD chmod stops parsing options at the
		// first operand, where "chmod 0644 -- file" would treat "--" as a filename.
		command = command.Raw("&&").Raw(sh.Command("chmod", "--", fmt.Sprintf("%#o", mode), name))
	}

	if err := s.Exec(command.String()); err != nil {
		return fmt.Errorf("mkdir %s: %w", name, err)
	}

	return nil
}

// Mkdir creates a new directory with the specified name and permission bits.
//
// The permission bits and the setgid, setuid and sticky bits of perm are
// applied; the file type bits are ignored.
func (s *PosixFS) Mkdir(name string, perm fs.FileMode) error {
	if err := s.Exec(sh.Command("mkdir", "-m", fmt.Sprintf("%#o", fileModeToPosixBits(perm)), name)); err != nil {
		return PathError("mkdir", name, err)
	}

	return nil
}

// WriteFile writes data to a file named by filename. Any missing parent
// directories are created.
//
// The file is created via a shell redirect instead of `install -m ... /dev/stdin`
// because the uutils (Rust) reimplementation of coreutils, the default on Ubuntu
// 25.10 and later, fails with "install: No such file or directory" when the
// source is /dev/stdin and the destination already exists — which is exactly
// what writing to a mktemp'd file does. See
// https://github.com/uutils/coreutils/issues/12407.
//
// The umask keeps a newly created file from being more permissive than perm
// while the content is written; the trailing chmod then applies perm's
// permission bits along with its setuid, setgid and sticky bits, also to a file
// that already existed. The file type bits of perm are ignored, as chmod has no
// representation for them. A umask only covers the nine permission bits, so the
// special bits come from the chmod alone — which is the safe ordering anyway, as
// the content is fully written by then. The umask is set after mkdir so that it
// does not affect the mode of the created parent directories.
//
// Missing parent directories get the remote default mode (0777 minus the remote
// umask) instead of the fixed 0755 that GNU `install -D` used. Use MkdirAll if
// the parent directories need a specific mode.
func (s *PosixFS) WriteFile(filename string, data []byte, perm fs.FileMode) error {
	mode := fileModeToPosixBits(perm)

	// "--" precedes the mode because BSD chmod stops parsing options at the
	// first operand, where "chmod 0644 -- file" would treat "--" as a filename.
	command := sh.CommandBuilder(sh.Command("mkdir", "-p", "--", s.Dir(filename))).
		Raw("&&").Raw(fmt.Sprintf("umask %#o", fs.ModePerm&^perm.Perm())).
		Raw("&&").Raw("cat").OutToFile(filename).
		Raw("&&").Raw(sh.Command("chmod", "--", fmt.Sprintf("%#o", mode), filename))

	if err := s.Exec(command.String(), cmd.Stdin(bytes.NewReader(data))); err != nil {
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
	out, err := s.ExecOutput(sh.Command("mktemp", "-d", "--", s.Join(dir, prefix+"XXXXXX")))
	if err != nil {
		return "", fmt.Errorf("mkdir temp %s: %w", dir, err)
	}
	return out, nil
}

// CreateTemp creates a new temporary file in the directory dir with a name beginning with prefix
// and returns the path of the new file. If dir is empty, TempDir() is used.
func (s *PosixFS) CreateTemp(dir, prefix string) (string, error) {
	if dir == "" {
		dir = s.TempDir()
	}
	out, err := s.ExecOutput(sh.Command("mktemp", "--", s.Join(dir, prefix+"XXXXXX")))
	if err != nil {
		return "", fmt.Errorf("create temp %s: %w", dir, err)
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

// Dir returns all but the last element of path, typically the path's directory.
func (s *PosixFS) Dir(p string) string {
	return path.Dir(p)
}

// Base returns the last element of path.
func (s *PosixFS) Base(p string) string {
	return path.Base(p)
}

// NativePath returns the path unchanged; on POSIX systems the native separator is already a forward slash.
func (s *PosixFS) NativePath(p string) string {
	return p
}

// ShellQuote returns a shell-escaped version of str, safe for use as a single argument in a POSIX shell command.
func (s *PosixFS) ShellQuote(str string) string {
	return shellescape.Quote(str)
}

// CommandExist reports whether the named command is available on the remote host.
func (s *PosixFS) CommandExist(name string) bool {
	_, err := s.LookPath(name)
	return err == nil
}

// isValidEnvVarName reports whether key is a valid POSIX environment variable name.
func isValidEnvVarName(key string) bool {
	if len(key) == 0 {
		return false
	}
	for i, r := range key {
		if r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

// Getenv returns the value of the environment variable named by the key.
func (s *PosixFS) Getenv(key string) string {
	if !isValidEnvVarName(key) {
		return ""
	}
	out, err := s.ExecOutput(fmt.Sprintf("printf '%%s' \"${%s}\"", key), cmd.HideOutput())
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

// MachineID returns the unique machine ID from /etc/machine-id.
func (s *PosixFS) MachineID() (string, error) {
	out, err := s.ExecOutput("cat /etc/machine-id")
	if err != nil {
		return "", fmt.Errorf("machine-id: %w", err)
	}
	if out == "" {
		return "", ErrEmptyMachineID
	}
	return out, nil
}

// SystemTime returns the current UTC time on the remote host.
// Note: date +%s is not POSIX but is supported on GNU coreutils, busybox, and macOS.
func (s *PosixFS) SystemTime() (time.Time, error) {
	out, err := s.ExecOutput("date -u +%s")
	if err != nil {
		return time.Time{}, fmt.Errorf("system time: %w", err)
	}
	secs, err := strconv.ParseInt(out, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("system time: parse %q: %w", out, err)
	}
	return time.Unix(secs, 0), nil
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

// Reboot triggers an immediate restart of the remote host. It first tries
// the simple 'reboot' command; if that fails with a logical error (not a
// transport tear-down), it falls back to 'shutdown -r now'. A
// transport-level error from either is treated as success since the kernel
// is already going down.
func (s *PosixFS) Reboot(ctx context.Context) error {
	if err := s.ExecContext(ctx, "reboot"); err != nil {
		if isTransportClosed(err) {
			return nil
		}
		if fallbackErr := s.ExecContext(ctx, sh.Command("shutdown", "-r", "now")); fallbackErr != nil {
			if isTransportClosed(fallbackErr) {
				return nil
			}
			return fmt.Errorf("%w (fallback shutdown -r now: %w)", err, fallbackErr)
		}
	}
	return nil
}
