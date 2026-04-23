package remotefs_test

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"testing"
	"time"

	"github.com/k0sproject/rig/v2/remotefs"
	"github.com/stretchr/testify/require"
)

// uploadFS is a minimal FS stub for Upload tests. Only the methods called by
// Upload are implemented; everything else panics if called.
type uploadFS struct {
	capturedPerm    fs.FileMode
	capturedChmod   fs.FileMode
	chmodCalled     bool
	written         []byte
}

func (f *uploadFS) OpenFile(_ string, _ int, perm fs.FileMode) (remotefs.File, error) {
	f.capturedPerm = perm
	return &uploadFile{buf: &f.written}, nil
}

func (f *uploadFS) Sha256(_ string) (string, error) {
	h := sha256.New()
	h.Write(f.written)
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (f *uploadFS) Chmod(_ string, mode fs.FileMode) error {
	f.chmodCalled = true
	f.capturedChmod = mode
	return nil
}

// FS interface stubs — not exercised by Upload.
func (f *uploadFS) Open(_ string) (fs.File, error)                           { panic("not implemented") }
func (f *uploadFS) Stat(_ string) (fs.FileInfo, error)                      { panic("not implemented") }
func (f *uploadFS) ReadFile(_ string) ([]byte, error)                       { panic("not implemented") }
func (f *uploadFS) ReadDir(_ string) ([]fs.DirEntry, error)                 { panic("not implemented") }
func (f *uploadFS) Remove(_ string) error                                   { panic("not implemented") }
func (f *uploadFS) RemoveAll(_ string) error                                { panic("not implemented") }
func (f *uploadFS) Mkdir(_ string, _ fs.FileMode) error                     { panic("not implemented") }
func (f *uploadFS) MkdirAll(_ string, _ fs.FileMode) error                  { panic("not implemented") }
func (f *uploadFS) MkdirTemp(_, _ string) (string, error)                   { panic("not implemented") }
func (f *uploadFS) WriteFile(_ string, _ []byte, _ fs.FileMode) error       { panic("not implemented") }
func (f *uploadFS) FileExist(_ string) bool                                 { panic("not implemented") }
func (f *uploadFS) LookPath(_ string) (string, error)                       { panic("not implemented") }
func (f *uploadFS) Join(_ ...string) string                                 { panic("not implemented") }
func (f *uploadFS) Chown(_ string, _ string) error                          { panic("not implemented") }
func (f *uploadFS) Chtimes(_ string, _, _ int64) error                      { panic("not implemented") }
func (f *uploadFS) Touch(_ string, _ ...time.Time) error                    { panic("not implemented") }
func (f *uploadFS) Truncate(_ string, _ int64) error                        { panic("not implemented") }
func (f *uploadFS) Getenv(_ string) string                                  { panic("not implemented") }
func (f *uploadFS) Rename(_, _ string) error                                { panic("not implemented") }
func (f *uploadFS) DownloadURL(_ string, _ string) error                    { panic("not implemented") }
func (f *uploadFS) FileContains(_ string, _ string) (bool, error)           { panic("not implemented") }
func (f *uploadFS) IsContainer() (bool, error)                              { panic("not implemented") }
func (f *uploadFS) Hostname() (string, error)                               { panic("not implemented") }
func (f *uploadFS) LongHostname() (string, error)                           { panic("not implemented") }
func (f *uploadFS) MachineID() (string, error)                              { panic("not implemented") }
func (f *uploadFS) SystemTime() (time.Time, error)                          { panic("not implemented") }
func (f *uploadFS) TempDir() string                                         { panic("not implemented") }
func (f *uploadFS) UserCacheDir() string                                    { panic("not implemented") }
func (f *uploadFS) UserConfigDir() string                                   { panic("not implemented") }
func (f *uploadFS) UserHomeDir() string                                     { panic("not implemented") }
func (f *uploadFS) Dir(_ string) string                                     { panic("not implemented") }
func (f *uploadFS) Base(_ string) string                                    { panic("not implemented") }
func (f *uploadFS) CommandExist(_ string) bool                              { panic("not implemented") }

// uploadFile is a minimal File stub that captures written bytes.
type uploadFile struct {
	buf *[]byte
}

func (f *uploadFile) CopyFrom(src io.Reader) (int64, error) {
	data, err := io.ReadAll(src)
	*f.buf = data
	return int64(len(data)), err
}

func (f *uploadFile) Close() error                              { return nil }
func (f *uploadFile) Name() string                             { return "" }
func (f *uploadFile) Read(_ []byte) (int, error)               { panic("not implemented") }
func (f *uploadFile) Seek(_ int64, _ int) (int64, error)       { panic("not implemented") }
func (f *uploadFile) Write(_ []byte) (int, error)              { panic("not implemented") }
func (f *uploadFile) Stat() (fs.FileInfo, error)               { panic("not implemented") }
func (f *uploadFile) CopyTo(_ io.Writer) (int64, error)        { panic("not implemented") }

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
	require.Equal(t, stat.Mode(), mfs.capturedPerm)
	require.False(t, mfs.chmodCalled, "Chmod should not be called without WithPermissions")
}

func TestUploadWithPermissions(t *testing.T) {
	src := writeTempFile(t, "hello", 0o644)
	mfs := &uploadFS{}

	require.NoError(t, remotefs.Upload(mfs, src, "/remote/dst", remotefs.WithPermissions(0o755)))
	require.Equal(t, fs.FileMode(0o755), mfs.capturedPerm)
	require.True(t, mfs.chmodCalled, "Chmod should be called with WithPermissions")
	require.Equal(t, fs.FileMode(0o755), mfs.capturedChmod)
}

func TestUploadChecksumMismatch(t *testing.T) {
	src := writeTempFile(t, "hello", 0o644)
	err := remotefs.Upload(&corruptUploadFS{uploadFS: uploadFS{}}, src, "/remote/dst")
	require.ErrorIs(t, err, remotefs.ErrChecksumMismatch)
}

// corruptUploadFS simulates a checksum mismatch by returning an incorrect sha256 digest.
type corruptUploadFS struct {
	uploadFS
}

func (f *corruptUploadFS) Sha256(_ string) (string, error) {
	return "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef", nil
}
