package remotefs_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/k0sproject/rig/v2/remotefs"
	"github.com/stretchr/testify/require"
)

// uploadFS is a minimal FS stub for Upload tests. Only the methods called by
// Upload are implemented; everything else panics if called.
type uploadFS struct {
	tmpPath       string
	capturedChmod fs.FileMode
	renamedFrom   string
	renamedTo     string
	removedPaths  []string
	written       []byte

	createTempErr error
	openFileErr   error
	copyFromErr   error
	closeErr      error
	sha256Err     error
	chmodErr      error
	renameErr     error
}

func (f *uploadFS) Dir(p string) string { return path.Dir(p) }

func (f *uploadFS) CreateTemp(dir, prefix string) (string, error) {
	if f.createTempErr != nil {
		return "", f.createTempErr
	}
	f.tmpPath = dir + "/" + prefix + "tmp"
	return f.tmpPath, nil
}

func (f *uploadFS) OpenFile(_ string, _ int, _ fs.FileMode) (remotefs.File, error) {
	if f.openFileErr != nil {
		return nil, f.openFileErr
	}
	return &uploadFile{buf: &f.written, copyFromErr: f.copyFromErr, closeErr: f.closeErr}, nil
}

func (f *uploadFS) Sha256(_ string) (string, error) {
	if f.sha256Err != nil {
		return "", f.sha256Err
	}
	h := sha256.New()
	h.Write(f.written)
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (f *uploadFS) Chmod(_ string, mode fs.FileMode) error {
	f.capturedChmod = mode
	return f.chmodErr
}

func (f *uploadFS) Rename(from, to string) error {
	f.renamedFrom = from
	f.renamedTo = to
	return f.renameErr
}

func (f *uploadFS) Remove(p string) error {
	f.removedPaths = append(f.removedPaths, p)
	return nil
}

// RemoveAll is what Upload's cleanup defer calls, so that a temp file already
// renamed away is not an error.
func (f *uploadFS) RemoveAll(p string) error {
	f.removedPaths = append(f.removedPaths, p)
	return nil
}

// FS interface stubs — not exercised by Upload.
func (f *uploadFS) Open(_ string) (fs.File, error)                        { panic("not implemented") }
func (f *uploadFS) Stat(_ string) (fs.FileInfo, error)                    { panic("not implemented") }
func (f *uploadFS) ReadFile(_ string) ([]byte, error)                     { panic("not implemented") }
func (f *uploadFS) ReadDir(_ string) ([]fs.DirEntry, error)               { panic("not implemented") }
func (f *uploadFS) Mkdir(_ string, _ fs.FileMode) error                   { panic("not implemented") }
func (f *uploadFS) MkdirAll(_ string, _ fs.FileMode) error                { panic("not implemented") }
func (f *uploadFS) MkdirTemp(_, _ string) (string, error)                 { panic("not implemented") }
func (f *uploadFS) WriteFile(_ string, _ []byte, _ fs.FileMode) error     { panic("not implemented") }
func (f *uploadFS) FileExist(_ string) bool                               { panic("not implemented") }
func (f *uploadFS) LookPath(_ string) (string, error)                     { panic("not implemented") }
func (f *uploadFS) Join(_ ...string) string                               { panic("not implemented") }
func (f *uploadFS) Chown(_ string, _ string) error                        { panic("not implemented") }
func (f *uploadFS) ChownInt(_ string, _, _ int) error                     { panic("not implemented") }
func (f *uploadFS) ChownTree(_ string, _ string) error                    { panic("not implemented") }
func (f *uploadFS) ChownTreeInt(_ string, _, _ int) error                 { panic("not implemented") }
func (f *uploadFS) Chtimes(_ string, _, _ int64) error                    { panic("not implemented") }
func (f *uploadFS) Touch(_ string, _ ...time.Time) error                  { panic("not implemented") }
func (f *uploadFS) Truncate(_ string, _ int64) error                      { panic("not implemented") }
func (f *uploadFS) Getenv(_ string) string                                { panic("not implemented") }
func (f *uploadFS) DownloadURL(_ string, _ string) error                  { panic("not implemented") }
func (f *uploadFS) FileContains(_ string, _ string) (bool, error)         { panic("not implemented") }
func (f *uploadFS) Follow(_ context.Context, _ string, _ io.Writer) error { panic("not implemented") }
func (f *uploadFS) IsContainer() (bool, error)                            { panic("not implemented") }
func (f *uploadFS) Hostname() (string, error)                             { panic("not implemented") }
func (f *uploadFS) LongHostname() (string, error)                         { panic("not implemented") }
func (f *uploadFS) MachineID() (string, error)                            { panic("not implemented") }
func (f *uploadFS) SystemTime() (time.Time, error)                        { panic("not implemented") }
func (f *uploadFS) TempDir() string                                       { panic("not implemented") }
func (f *uploadFS) UserCacheDir() string                                  { panic("not implemented") }
func (f *uploadFS) UserConfigDir() string                                 { panic("not implemented") }
func (f *uploadFS) UserHomeDir() string                                   { panic("not implemented") }
func (f *uploadFS) Base(_ string) string                                  { panic("not implemented") }
func (f *uploadFS) CommandExist(_ string) bool                            { panic("not implemented") }
func (f *uploadFS) Reboot(_ context.Context) error                        { panic("not implemented") }
func (f *uploadFS) NativePath(_ string) string                            { panic("not implemented") }
func (f *uploadFS) ShellQuote(_ string) string                            { panic("not implemented") }

// uploadFile is a minimal File stub that captures written bytes.
type uploadFile struct {
	buf         *[]byte
	copyFromErr error
	closeErr    error
}

func (f *uploadFile) CopyFrom(src io.Reader) (int64, error) {
	data, err := io.ReadAll(src)
	*f.buf = data
	if f.copyFromErr != nil {
		return int64(len(data)), f.copyFromErr
	}
	return int64(len(data)), err
}

func (f *uploadFile) Close() error                       { return f.closeErr }
func (f *uploadFile) Name() string                       { return "" }
func (f *uploadFile) Read(_ []byte) (int, error)         { panic("not implemented") }
func (f *uploadFile) Seek(_ int64, _ int) (int64, error) { panic("not implemented") }
func (f *uploadFile) Write(_ []byte) (int, error)        { panic("not implemented") }
func (f *uploadFile) Stat() (fs.FileInfo, error)         { panic("not implemented") }
func (f *uploadFile) CopyTo(_ io.Writer) (int64, error)  { panic("not implemented") }

func writeTempFile(t *testing.T, content string, mode fs.FileMode) string {
	t.Helper()
	tmp, err := os.CreateTemp(t.TempDir(), "upload-test-*")
	require.NoError(t, err)
	_, err = tmp.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, tmp.Chmod(mode))
	require.NoError(t, tmp.Close())
	return tmp.Name()
}

func TestUploadUsesLocalPermByDefault(t *testing.T) {
	src := writeTempFile(t, "hello", 0o755)

	// Stat after creation to get the actual mode (Windows normalises POSIX bits).
	stat, err := os.Stat(src)
	require.NoError(t, err)

	mfs := &uploadFS{}
	require.NoError(t, remotefs.Upload(mfs, src, "/remote/dst"))
	require.Equal(t, stat.Mode(), mfs.capturedChmod, "Chmod must be called with local file's mode")
	require.Equal(t, mfs.tmpPath, mfs.renamedFrom, "temp path must be renamed to dst")
	require.Equal(t, "/remote/dst", mfs.renamedTo)
}

func TestUploadWithPermissions(t *testing.T) {
	src := writeTempFile(t, "hello", 0o644)
	mfs := &uploadFS{}

	require.NoError(t, remotefs.Upload(mfs, src, "/remote/dst", remotefs.WithPermissions(0o755)))
	require.Equal(t, fs.FileMode(0o755), mfs.capturedChmod, "Chmod must be called with specified mode")
	require.Equal(t, mfs.tmpPath, mfs.renamedFrom)
	require.Equal(t, "/remote/dst", mfs.renamedTo)
}

func TestUploadChecksumMismatch(t *testing.T) {
	src := writeTempFile(t, "hello", 0o644)
	corruptFS := &corruptUploadFS{}
	err := remotefs.Upload(corruptFS, src, "/remote/dst")
	require.ErrorIs(t, err, remotefs.ErrChecksumMismatch)
	require.Contains(t, corruptFS.removedPaths, corruptFS.tmpPath, "temp file must be removed on checksum mismatch")
}

func TestUploadCopyFailureCleansTempFile(t *testing.T) {
	src := writeTempFile(t, "hello", 0o644)
	mfs := &uploadFS{copyFromErr: errors.New("network error")}

	err := remotefs.Upload(mfs, src, "/remote/dst")
	require.Error(t, err)
	require.Contains(t, mfs.removedPaths, mfs.tmpPath, "temp file must be removed after copy failure")
}

func TestUploadSessionTimeoutSkipsCleanup(t *testing.T) {
	src := writeTempFile(t, "hello", 0o644)
	mfs := &uploadFS{copyFromErr: fmt.Errorf("copy file to remote host: %w", remotefs.ErrTimeout)}

	err := remotefs.Upload(mfs, src, "/remote/dst")

	require.ErrorIs(t, err, remotefs.ErrTimeout)
	require.Empty(t, mfs.removedPaths, "cleanup must not run on a session that timed out: the connection is gone and the command would block on it")
}

func TestUploadCancelledSkipsCleanup(t *testing.T) {
	src := writeTempFile(t, "hello", 0o644)
	mfs := &uploadFS{copyFromErr: fmt.Errorf("copy file to remote host: %w", context.Canceled)}

	err := remotefs.Upload(mfs, src, "/remote/dst")

	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, mfs.removedPaths, "cleanup must not run once the bound context has ended: it would only fail too")
}

func TestUploadKeepsATimedOutCloseBehindACopyFailure(t *testing.T) {
	src := writeTempFile(t, "hello", 0o644)
	// The copy fails for its own reasons and the close then finds the session
	// gone. The close is the one that decides whether cleanup can run at all,
	// so it must not be dropped in favour of the copy error.
	mfs := &uploadFS{
		copyFromErr: errors.New("network error"),
		closeErr:    fmt.Errorf("close temp file: %w", remotefs.ErrTimeout),
	}

	err := remotefs.Upload(mfs, src, "/remote/dst")

	require.ErrorIs(t, err, remotefs.ErrTimeout)
	require.Empty(t, mfs.removedPaths, "cleanup must not run over a session that timed out")
}

func TestUploadRenameFailureCleansTempFile(t *testing.T) {
	src := writeTempFile(t, "hello", 0o644)
	mfs := &uploadFS{renameErr: errors.New("cross-device link")}

	err := remotefs.Upload(mfs, src, "/remote/dst")
	require.Error(t, err)
	require.Contains(t, mfs.removedPaths, mfs.tmpPath, "temp file must be removed after rename failure")
}

func TestUploadCreateTempFailure(t *testing.T) {
	src := writeTempFile(t, "hello", 0o644)
	mfs := &uploadFS{createTempErr: errors.New("no space left on device")}

	err := remotefs.Upload(mfs, src, "/remote/dst")
	require.Error(t, err)
	require.Empty(t, mfs.removedPaths, "no temp file created, nothing to remove")
}

// corruptUploadFS simulates a checksum mismatch by returning an incorrect sha256 digest.
type corruptUploadFS struct {
	uploadFS
}

func (f *corruptUploadFS) Sha256(_ string) (string, error) {
	return "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef", nil
}

// downloadFS is a minimal FS stub for DownloadFile tests. It reuses uploadFS
// for the methods DownloadFile never calls.
type downloadFS struct {
	uploadFS

	content []byte
	mode    fs.FileMode
	modTime time.Time

	statErr      error
	copyToErr    error
	closeErr     error
	sum          string // overrides the computed checksum when set
	opened       int
	closed       int
	sumWhileOpen bool
}

func (f *downloadFS) Stat(name string) (fs.FileInfo, error) {
	if f.statErr != nil {
		return nil, f.statErr
	}
	return fileInfo{name: filepath.Base(name), size: int64(len(f.content)), mode: f.mode, modTime: f.modTime}, nil
}

func (f *downloadFS) OpenFile(_ string, _ int, _ fs.FileMode) (remotefs.File, error) {
	f.opened++
	return &downloadFile{fs: f, content: f.content, copyToErr: f.copyToErr}, nil
}

// Sha256 fails while the file is still open, as WinFS does: the rigrcp daemon
// opens files without sharing, so a second open is a sharing violation.
func (f *downloadFS) Sha256(_ string) (string, error) {
	if f.opened > f.closed {
		f.sumWhileOpen = true
		return "", errors.New("the process cannot access the file because it is being used by another process")
	}
	if f.sum != "" {
		return f.sum, nil
	}
	h := sha256.Sum256(f.content)
	return hex.EncodeToString(h[:]), nil
}

type downloadFile struct {
	uploadFile

	fs        *downloadFS
	content   []byte
	copyToErr error
}

func (f *downloadFile) CopyTo(dst io.Writer) (int64, error) {
	if f.copyToErr != nil {
		// Write part of the content first, as a transfer cut short would.
		n, _ := dst.Write(f.content[:len(f.content)/2])
		return int64(n), f.copyToErr
	}
	n, err := dst.Write(f.content)
	return int64(n), err
}

func (f *downloadFile) Close() error {
	f.fs.closed++
	return f.fs.closeErr
}

type fileInfo struct {
	name    string
	size    int64
	mode    fs.FileMode
	modTime time.Time
}

func (fi fileInfo) Name() string       { return fi.name }
func (fi fileInfo) Size() int64        { return fi.size }
func (fi fileInfo) Mode() fs.FileMode  { return fi.mode }
func (fi fileInfo) ModTime() time.Time { return fi.modTime }
func (fi fileInfo) IsDir() bool        { return fi.mode.IsDir() }
func (fi fileInfo) Sys() any           { return nil }

func newDownloadFS(content string) *downloadFS {
	return &downloadFS{
		content: []byte(content),
		mode:    0o640,
		modTime: time.Date(2024, 5, 4, 12, 30, 15, 0, time.UTC),
	}
}

// requireOnlyFile asserts that dir holds dst and nothing else, so no
// temporary file was left behind.
func requireOnlyFile(t *testing.T, dst string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(dst))
	require.NoError(t, err)
	require.Len(t, entries, 1, "only the destination file may remain in the directory")
	require.Equal(t, filepath.Base(dst), entries[0].Name())
}

func TestDownloadFile(t *testing.T) {
	mfs := newDownloadFS("hello world")
	dst := filepath.Join(t.TempDir(), "dst")

	require.NoError(t, remotefs.DownloadFile(mfs, "/remote/src", dst))

	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	require.Equal(t, "hello world", string(got))
	stat, err := os.Stat(dst)
	require.NoError(t, err)
	require.True(t, stat.ModTime().Equal(mfs.modTime), "local mtime %s must match the remote %s", stat.ModTime(), mfs.modTime)
	if runtime.GOOS != "windows" {
		require.Equal(t, fs.FileMode(0o640), stat.Mode().Perm(), "local mode must match the remote file's")
	}
	requireOnlyFile(t, dst)
	require.False(t, mfs.sumWhileOpen, "the remote file must be closed before its checksum is taken")
	require.Equal(t, 1, mfs.closed, "the remote file must be closed exactly once")
}

func TestDownloadFileRemoteCloseFailure(t *testing.T) {
	mfs := newDownloadFS("hello")
	mfs.closeErr = errors.New("daemon gone")
	dst := filepath.Join(t.TempDir(), "dst")

	err := remotefs.DownloadFile(mfs, "/remote/src", dst)

	require.ErrorIs(t, err, mfs.closeErr)
	require.Equal(t, 1, mfs.closed, "a failed close must not be retried by the deferred cleanup")
	require.NoFileExists(t, dst)
	entries, readErr := os.ReadDir(filepath.Dir(dst))
	require.NoError(t, readErr)
	require.Empty(t, entries, "the temp file must be removed")
}

func TestDownloadFileCopyFailureClosesRemote(t *testing.T) {
	mfs := newDownloadFS("hello")
	mfs.copyToErr = errors.New("connection reset")

	require.Error(t, remotefs.DownloadFile(mfs, "/remote/src", filepath.Join(t.TempDir(), "dst")))
	require.Equal(t, 1, mfs.closed, "the remote file must be closed on an early failure too")
}

func TestDownloadFileWithPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows only keeps a read-only flag")
	}
	mfs := newDownloadFS("hello")
	dst := filepath.Join(t.TempDir(), "dst")

	require.NoError(t, remotefs.DownloadFile(mfs, "/remote/src", dst, remotefs.WithPermissions(0o600)))

	stat, err := os.Stat(dst)
	require.NoError(t, err)
	require.Equal(t, fs.FileMode(0o600), stat.Mode().Perm())
}

func TestDownloadFileReplacesExisting(t *testing.T) {
	mfs := newDownloadFS("new content")
	dst := filepath.Join(t.TempDir(), "dst")
	require.NoError(t, os.WriteFile(dst, []byte("old"), 0o600))

	require.NoError(t, remotefs.DownloadFile(mfs, "/remote/src", dst))

	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	require.Equal(t, "new content", string(got))
	requireOnlyFile(t, dst)
}

func TestDownloadFileChecksumMismatch(t *testing.T) {
	mfs := newDownloadFS("hello")
	mfs.sum = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	dst := filepath.Join(t.TempDir(), "dst")
	require.NoError(t, os.WriteFile(dst, []byte("old"), 0o600))

	err := remotefs.DownloadFile(mfs, "/remote/src", dst)

	require.ErrorIs(t, err, remotefs.ErrChecksumMismatch)
	got, readErr := os.ReadFile(dst)
	require.NoError(t, readErr)
	require.Equal(t, "old", string(got), "dst must be left as it was")
	requireOnlyFile(t, dst)
}

func TestDownloadFileCopyFailure(t *testing.T) {
	mfs := newDownloadFS("hello world")
	mfs.copyToErr = errors.New("connection reset")
	dst := filepath.Join(t.TempDir(), "dst")
	require.NoError(t, os.WriteFile(dst, []byte("old"), 0o600))

	err := remotefs.DownloadFile(mfs, "/remote/src", dst)

	require.ErrorIs(t, err, mfs.copyToErr)
	got, readErr := os.ReadFile(dst)
	require.NoError(t, readErr)
	require.Equal(t, "old", string(got), "a partial transfer must not reach dst")
	requireOnlyFile(t, dst)
}

func TestDownloadFileStatFailure(t *testing.T) {
	mfs := newDownloadFS("hello")
	mfs.statErr = &fs.PathError{Op: "stat", Path: "/remote/src", Err: fs.ErrNotExist}
	dst := filepath.Join(t.TempDir(), "dst")

	err := remotefs.DownloadFile(mfs, "/remote/src", dst)

	require.ErrorIs(t, err, fs.ErrNotExist)
	require.Zero(t, mfs.opened)
	require.NoFileExists(t, dst)
}

func TestDownloadFileRejectsNonRegular(t *testing.T) {
	for name, mode := range map[string]fs.FileMode{
		"directory": fs.ModeDir | 0o755,
		"symlink":   fs.ModeSymlink | 0o777,
	} {
		t.Run(name, func(t *testing.T) {
			mfs := newDownloadFS("")
			mfs.mode = mode
			dst := filepath.Join(t.TempDir(), "dst")

			err := remotefs.DownloadFile(mfs, "/remote/src", dst)

			require.ErrorIs(t, err, fs.ErrInvalid)
			require.Zero(t, mfs.opened, "a non-regular file must not be opened")
			require.NoFileExists(t, dst)
		})
	}
}
