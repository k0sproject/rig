package remotefs_test

import (
	"errors"
	"io/fs"
	"path"
	"testing"
	"time"

	"github.com/k0sproject/rig/v2/remotefs"
	"github.com/k0sproject/rig/v2/rigtest"
	"github.com/stretchr/testify/require"
)

// atomicOS is a minimal OS stub for WriteFileAtomic tests.
// Only the seven methods called by WriteFileAtomic are implemented;
// everything else panics so a mistaken call surfaces immediately.
type atomicOS struct {
	mkdirAllErr    error
	createTempPath string
	createTempErr  error
	writeFileErr   error
	chmodErr       error
	renameErr      error

	removedPaths []string
}

func (o *atomicOS) Dir(p string) string                               { return path.Dir(p) }
func (o *atomicOS) MkdirAll(_ string, _ fs.FileMode) error            { return o.mkdirAllErr }
func (o *atomicOS) CreateTemp(_, _ string) (string, error)            { return o.createTempPath, o.createTempErr }
func (o *atomicOS) WriteFile(_ string, _ []byte, _ fs.FileMode) error { return o.writeFileErr }
func (o *atomicOS) Chmod(_ string, _ fs.FileMode) error               { return o.chmodErr }
func (o *atomicOS) Rename(_, _ string) error                          { return o.renameErr }
func (o *atomicOS) Remove(p string) error                             { o.removedPaths = append(o.removedPaths, p); return nil }

// Unused methods — panic on call so misuse is caught immediately.
func (o *atomicOS) RemoveAll(_ string) error               { panic("not implemented") }
func (o *atomicOS) Mkdir(_ string, _ fs.FileMode) error    { panic("not implemented") }
func (o *atomicOS) MkdirTemp(_, _ string) (string, error)  { panic("not implemented") }
func (o *atomicOS) FileExist(_ string) bool                { panic("not implemented") }
func (o *atomicOS) LookPath(_ string) (string, error)      { panic("not implemented") }
func (o *atomicOS) Join(_ ...string) string                { panic("not implemented") }
func (o *atomicOS) Chown(_ string, _ string) error         { panic("not implemented") }
func (o *atomicOS) ChownInt(_ string, _, _ int) error      { panic("not implemented") }
func (o *atomicOS) ChownTree(_ string, _ string) error     { panic("not implemented") }
func (o *atomicOS) ChownTreeInt(_ string, _, _ int) error  { panic("not implemented") }
func (o *atomicOS) Chtimes(_ string, _, _ int64) error     { panic("not implemented") }
func (o *atomicOS) Touch(_ string, _ ...time.Time) error   { panic("not implemented") }
func (o *atomicOS) Truncate(_ string, _ int64) error       { panic("not implemented") }
func (o *atomicOS) Getenv(_ string) string                 { panic("not implemented") }
func (o *atomicOS) FileContains(_, _ string) (bool, error) { panic("not implemented") }
func (o *atomicOS) IsContainer() (bool, error)             { panic("not implemented") }
func (o *atomicOS) Hostname() (string, error)              { panic("not implemented") }
func (o *atomicOS) LongHostname() (string, error)          { panic("not implemented") }
func (o *atomicOS) MachineID() (string, error)             { panic("not implemented") }
func (o *atomicOS) SystemTime() (time.Time, error)         { panic("not implemented") }
func (o *atomicOS) TempDir() string                        { panic("not implemented") }
func (o *atomicOS) UserCacheDir() string                   { panic("not implemented") }
func (o *atomicOS) UserConfigDir() string                  { panic("not implemented") }
func (o *atomicOS) UserHomeDir() string                    { panic("not implemented") }
func (o *atomicOS) Base(_ string) string                   { panic("not implemented") }
func (o *atomicOS) CommandExist(_ string) bool             { panic("not implemented") }

// Compile-time check that atomicOS satisfies remotefs.OS.
var _ remotefs.OS = (*atomicOS)(nil)

func TestWriteFileAtomic(t *testing.T) {
	const target = "/srv/k0s"
	const tmp = "/srv/.tmp-abc123"
	data := []byte("binary content")

	t.Run("success", func(t *testing.T) {
		o := &atomicOS{createTempPath: tmp}
		err := remotefs.WriteFileAtomic(o, target, data, 0o755)
		require.NoError(t, err)
		// After a successful rename the temp path no longer exists; Remove is
		// still called by defer but the error is ignored.
		require.Contains(t, o.removedPaths, tmp, "deferred Remove must be called")
	})

	t.Run("write failure cleans up temp", func(t *testing.T) {
		o := &atomicOS{
			createTempPath: tmp,
			writeFileErr:   errors.New("no space left on device"),
		}
		err := remotefs.WriteFileAtomic(o, target, data, 0o755)
		require.Error(t, err)
		require.Contains(t, o.removedPaths, tmp, "temp file must be removed after write failure")
	})

	t.Run("chmod failure cleans up temp", func(t *testing.T) {
		o := &atomicOS{
			createTempPath: tmp,
			chmodErr:       errors.New("operation not permitted"),
		}
		err := remotefs.WriteFileAtomic(o, target, data, 0o755)
		require.Error(t, err)
		require.Contains(t, o.removedPaths, tmp, "temp file must be removed after chmod failure")
	})

	t.Run("rename failure cleans up temp", func(t *testing.T) {
		o := &atomicOS{
			createTempPath: tmp,
			renameErr:      errors.New("cross-device link"),
		}
		err := remotefs.WriteFileAtomic(o, target, data, 0o755)
		require.Error(t, err)
		require.Contains(t, o.removedPaths, tmp, "temp file must be removed after rename failure")
	})

	t.Run("mkdirall failure propagates", func(t *testing.T) {
		o := &atomicOS{mkdirAllErr: errors.New("permission denied")}
		err := remotefs.WriteFileAtomic(o, target, data, 0o755)
		require.Error(t, err)
		require.Empty(t, o.removedPaths, "no temp file created, nothing to remove")
	})

	t.Run("createtemp failure propagates", func(t *testing.T) {
		o := &atomicOS{createTempErr: errors.New("no space left on device")}
		err := remotefs.WriteFileAtomic(o, target, data, 0o755)
		require.Error(t, err)
		require.Empty(t, o.removedPaths)
	})
}

// TestWriteFileAtomicPosix verifies WriteFileAtomic works end-to-end via PosixFS.
func TestWriteFileAtomicPosix(t *testing.T) {
	mr := rigtest.NewMockRunner()
	// initStat probe
	mr.AddCommandOutput(rigtest.Equal("stat --help 2>&1"), "Usage: stat --format=FORMAT")
	// MkdirAll: Stat probe returns empty (dir not found), then install -d
	mr.AddCommandSuccess(rigtest.Contains("stat -c"))
	mr.AddCommandSuccess(rigtest.Contains("install -d"))
	// CreateTemp
	mr.AddCommandOutput(rigtest.Contains("mktemp"), "/srv/.tmp-abc123")
	// WriteFile via install + stdin
	mr.AddCommandSuccess(rigtest.Contains("install"))
	// Chmod
	mr.AddCommandSuccess(rigtest.Contains("chmod"))
	// Rename
	mr.AddCommandSuccess(rigtest.Contains("mv -f"))

	f := remotefs.NewPosixFS(mr)
	err := remotefs.WriteFileAtomic(f, "/srv/k0s", []byte("binary"), 0o755)
	require.NoError(t, err)
	require.NoError(t, mr.Received(rigtest.Contains("mktemp")))
	require.NoError(t, mr.Received(rigtest.Contains("chmod")))
	require.NoError(t, mr.Received(rigtest.Contains("mv -f")))
}
