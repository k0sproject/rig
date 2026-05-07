package remotefs_test

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"path"
	"testing"
	"time"

	"github.com/k0sproject/rig/v2/remotefs"
	"github.com/k0sproject/rig/v2/rigtest"
	"github.com/stretchr/testify/require"
)

// patchFS is a minimal FS stub for PatchFile tests. The methods called by
// PatchFile and WriteFileAtomic are implemented; everything else panics.
type patchFS struct {
	content  []byte
	statErr  error
	statMode fs.FileMode

	written      []byte
	writtenPerm  fs.FileMode
	writeCalled  bool
}

func (f *patchFS) Stat(_ string) (fs.FileInfo, error) {
	if f.statErr != nil {
		return nil, f.statErr
	}
	return &patchFileInfo{mode: f.statMode}, nil
}

func (f *patchFS) ReadFile(_ string) ([]byte, error) {
	return f.content, nil
}

// WriteFileAtomic delegates to these OS methods:
func (f *patchFS) Dir(p string) string                             { return path.Dir(p) }
func (f *patchFS) MkdirAll(_ string, _ fs.FileMode) error         { return nil }
func (f *patchFS) CreateTemp(dir, _ string) (string, error)       { return dir + "/.tmp-test", nil }
func (f *patchFS) WriteFile(_ string, data []byte, _ fs.FileMode) error {
	f.written = data
	f.writeCalled = true
	return nil
}
func (f *patchFS) Chmod(_ string, mode fs.FileMode) error { f.writtenPerm = mode; return nil }
func (f *patchFS) Rename(_, _ string) error               { return nil }
func (f *patchFS) Remove(_ string) error                  { return nil }

// Unused FS methods — panic on call so any unexpected usage is caught immediately.
func (f *patchFS) Open(_ string) (fs.File, error)                          { panic("not implemented") }
func (f *patchFS) ReadDir(_ string) ([]fs.DirEntry, error)                 { panic("not implemented") }
func (f *patchFS) OpenFile(_ string, _ int, _ fs.FileMode) (remotefs.File, error) {
	panic("not implemented")
}
func (f *patchFS) Sha256(_ string) (string, error)                         { panic("not implemented") }
func (f *patchFS) DownloadURL(_, _ string) error                           { panic("not implemented") }
func (f *patchFS) RoundTrip(_ *http.Request) (*http.Response, error)       { panic("not implemented") }
func (f *patchFS) RemoveAll(_ string) error                                { panic("not implemented") }
func (f *patchFS) Mkdir(_ string, _ fs.FileMode) error                     { panic("not implemented") }
func (f *patchFS) MkdirTemp(_, _ string) (string, error)                   { panic("not implemented") }
func (f *patchFS) FileExist(_ string) bool                                 { panic("not implemented") }
func (f *patchFS) LookPath(_ string) (string, error)                       { panic("not implemented") }
func (f *patchFS) Join(_ ...string) string                                 { panic("not implemented") }
func (f *patchFS) Chown(_ string, _ string) error                          { panic("not implemented") }
func (f *patchFS) ChownInt(_ string, _, _ int) error                       { panic("not implemented") }
func (f *patchFS) ChownTree(_ string, _ string) error                      { panic("not implemented") }
func (f *patchFS) ChownTreeInt(_ string, _, _ int) error                   { panic("not implemented") }
func (f *patchFS) Chtimes(_ string, _, _ int64) error                      { panic("not implemented") }
func (f *patchFS) Touch(_ string, _ ...time.Time) error                    { panic("not implemented") }
func (f *patchFS) Truncate(_ string, _ int64) error                        { panic("not implemented") }
func (f *patchFS) Getenv(_ string) string                                  { panic("not implemented") }
func (f *patchFS) FileContains(_, _ string) (bool, error)                  { panic("not implemented") }
func (f *patchFS) Follow(_ context.Context, _ string, _ io.Writer) error   { panic("not implemented") }
func (f *patchFS) IsContainer() (bool, error)                              { panic("not implemented") }
func (f *patchFS) Hostname() (string, error)                               { panic("not implemented") }
func (f *patchFS) LongHostname() (string, error)                           { panic("not implemented") }
func (f *patchFS) MachineID() (string, error)                              { panic("not implemented") }
func (f *patchFS) SystemTime() (time.Time, error)                          { panic("not implemented") }
func (f *patchFS) TempDir() string                                         { panic("not implemented") }
func (f *patchFS) UserCacheDir() string                                    { panic("not implemented") }
func (f *patchFS) UserConfigDir() string                                   { panic("not implemented") }
func (f *patchFS) UserHomeDir() string                                     { panic("not implemented") }
func (f *patchFS) Base(_ string) string                                    { panic("not implemented") }
func (f *patchFS) CommandExist(_ string) bool                              { panic("not implemented") }

var _ remotefs.FS = (*patchFS)(nil)

// patchFileInfo is a minimal fs.FileInfo stub.
type patchFileInfo struct{ mode fs.FileMode }

func (i *patchFileInfo) Name() string      { return "stub" }
func (i *patchFileInfo) Size() int64       { return 0 }
func (i *patchFileInfo) Mode() fs.FileMode { return i.mode }
func (i *patchFileInfo) ModTime() time.Time { return time.Time{} }
func (i *patchFileInfo) IsDir() bool       { return false }
func (i *patchFileInfo) Sys() any          { return nil }

func newPatchFS(content string) *patchFS {
	return &patchFS{content: []byte(content), statMode: 0o644}
}

// writtenStr returns what PatchFile wrote, as a string.
func (f *patchFS) writtenStr() string { return string(f.written) }

// wasWritten reports whether WriteFileAtomic was called (i.e. the file was changed).
func (f *patchFS) wasWritten() bool { return f.writeCalled }

func TestPatchFileReplaceOrAppend(t *testing.T) {
	t.Run("replaces first matching line", func(t *testing.T) {
		f := newPatchFS("FOO=old\nBAR=keep\n")
		err := remotefs.PatchFile(f, "/etc/env", []remotefs.Patch{
			remotefs.ReplaceOrAppend(remotefs.ByPrefix("FOO="), "FOO=new"),
		})
		require.NoError(t, err)
		require.Equal(t, "FOO=new\nBAR=keep\n", f.writtenStr())
	})

	t.Run("appends when no line matches", func(t *testing.T) {
		f := newPatchFS("BAR=keep\n")
		err := remotefs.PatchFile(f, "/etc/env", []remotefs.Patch{
			remotefs.ReplaceOrAppend(remotefs.ByPrefix("FOO="), "FOO=new"),
		})
		require.NoError(t, err)
		require.Equal(t, "BAR=keep\nFOO=new\n", f.writtenStr())
	})

	t.Run("appends to empty file", func(t *testing.T) {
		// An existing empty file has no trailing newline, so neither does the output.
		f := newPatchFS("")
		err := remotefs.PatchFile(f, "/etc/env", []remotefs.Patch{
			remotefs.ReplaceOrAppend(remotefs.ByPrefix("FOO="), "FOO=new"),
		})
		require.NoError(t, err)
		require.Equal(t, "FOO=new", f.writtenStr())
	})
}

func TestPatchFileDeleteMatching(t *testing.T) {
	t.Run("removes matching lines", func(t *testing.T) {
		f := newPatchFS("keep\ndelete-me\nalso-delete\nkeep-too\n")
		err := remotefs.PatchFile(f, "/etc/env", []remotefs.Patch{
			remotefs.DeleteMatching(remotefs.ByContains("delete")),
		})
		require.NoError(t, err)
		require.Equal(t, "keep\nkeep-too\n", f.writtenStr())
	})

	t.Run("no-op when no lines match", func(t *testing.T) {
		f := newPatchFS("keep\nkeep-too\n")
		err := remotefs.PatchFile(f, "/etc/env", []remotefs.Patch{
			remotefs.DeleteMatching(remotefs.ByContains("delete")),
		})
		require.NoError(t, err)
		require.False(t, f.wasWritten(), "no-op patch should not write the file")
	})
}

func TestPatchFileAppendIfMissing(t *testing.T) {
	t.Run("appends when line is absent", func(t *testing.T) {
		f := newPatchFS("existing\n")
		err := remotefs.PatchFile(f, "/etc/env", []remotefs.Patch{
			remotefs.AppendIfMissing("newline"),
		})
		require.NoError(t, err)
		require.Equal(t, "existing\nnewline\n", f.writtenStr())
	})

	t.Run("no-op when line is already present", func(t *testing.T) {
		f := newPatchFS("existing\nnewline\n")
		err := remotefs.PatchFile(f, "/etc/env", []remotefs.Patch{
			remotefs.AppendIfMissing("newline"),
		})
		require.NoError(t, err)
		require.False(t, f.wasWritten(), "no-op patch should not write the file")
	})
}

func TestPatchFileInsertAfter(t *testing.T) {
	t.Run("inserts after matching line", func(t *testing.T) {
		f := newPatchFS("line1\nanchor\nline3\n")
		err := remotefs.PatchFile(f, "/etc/env", []remotefs.Patch{
			remotefs.InsertAfter(remotefs.ByExact("anchor"), "inserted"),
		})
		require.NoError(t, err)
		require.Equal(t, "line1\nanchor\ninserted\nline3\n", f.writtenStr())
	})

	t.Run("no-op when no line matches", func(t *testing.T) {
		f := newPatchFS("line1\nline2\n")
		err := remotefs.PatchFile(f, "/etc/env", []remotefs.Patch{
			remotefs.InsertAfter(remotefs.ByExact("anchor"), "inserted"),
		})
		require.NoError(t, err)
		require.False(t, f.wasWritten(), "no-op patch should not write the file")
	})

	t.Run("inserts after last line", func(t *testing.T) {
		f := newPatchFS("line1\nanchor\n")
		err := remotefs.PatchFile(f, "/etc/env", []remotefs.Patch{
			remotefs.InsertAfter(remotefs.ByExact("anchor"), "inserted"),
		})
		require.NoError(t, err)
		require.Equal(t, "line1\nanchor\ninserted\n", f.writtenStr())
	})
}

func TestPatchFileInsertBefore(t *testing.T) {
	t.Run("inserts before matching line", func(t *testing.T) {
		f := newPatchFS("line1\nanchor\nline3\n")
		err := remotefs.PatchFile(f, "/etc/env", []remotefs.Patch{
			remotefs.InsertBefore(remotefs.ByExact("anchor"), "inserted"),
		})
		require.NoError(t, err)
		require.Equal(t, "line1\ninserted\nanchor\nline3\n", f.writtenStr())
	})

	t.Run("no-op when no line matches", func(t *testing.T) {
		f := newPatchFS("line1\nline2\n")
		err := remotefs.PatchFile(f, "/etc/env", []remotefs.Patch{
			remotefs.InsertBefore(remotefs.ByExact("anchor"), "inserted"),
		})
		require.NoError(t, err)
		require.False(t, f.wasWritten(), "no-op patch should not write the file")
	})

	t.Run("inserts before first line", func(t *testing.T) {
		f := newPatchFS("anchor\nline2\n")
		err := remotefs.PatchFile(f, "/etc/env", []remotefs.Patch{
			remotefs.InsertBefore(remotefs.ByExact("anchor"), "inserted"),
		})
		require.NoError(t, err)
		require.Equal(t, "inserted\nanchor\nline2\n", f.writtenStr())
	})
}

func TestPatchFileTransform(t *testing.T) {
	f := newPatchFS("a\nb\nc\n")
	err := remotefs.PatchFile(f, "/etc/env", []remotefs.Patch{
		remotefs.Transform(func(lines []string) ([]string, error) {
			return []string{"x", "y"}, nil
		}),
	})
	require.NoError(t, err)
	require.Equal(t, "x\ny\n", f.writtenStr())
}

func TestPatchFileMultiplePatches(t *testing.T) {
	f := newPatchFS("FOO=old\nBAR=keep\nDELETE_ME=1\n")
	err := remotefs.PatchFile(f, "/etc/env", []remotefs.Patch{
		remotefs.ReplaceOrAppend(remotefs.ByPrefix("FOO="), "FOO=new"),
		remotefs.DeleteMatching(remotefs.ByPrefix("DELETE_ME=")),
		remotefs.AppendIfMissing("BAZ=added"),
	})
	require.NoError(t, err)
	require.Equal(t, "FOO=new\nBAR=keep\nBAZ=added\n", f.writtenStr())
}

func TestPatchFileCRLF(t *testing.T) {
	f := newPatchFS("line1\r\nline2\r\n")
	err := remotefs.PatchFile(f, "/etc/env", []remotefs.Patch{
		remotefs.ReplaceOrAppend(remotefs.ByExact("line1"), "LINE1"),
	})
	require.NoError(t, err)
	// CRLF must be preserved in the output.
	require.Equal(t, "LINE1\r\nline2\r\n", f.writtenStr())
}

func TestPatchFilePreservesPermissions(t *testing.T) {
	f := &patchFS{content: []byte("line\n"), statMode: 0o755}
	err := remotefs.PatchFile(f, "/etc/script", []remotefs.Patch{
		remotefs.AppendIfMissing("extra"),
	})
	require.NoError(t, err)
	require.Equal(t, fs.FileMode(0o755), f.writtenPerm)
}

func TestPatchFileWithCreate(t *testing.T) {
	t.Run("creates file when missing", func(t *testing.T) {
		f := &patchFS{statErr: &fs.PathError{Op: "stat", Path: "/new", Err: fs.ErrNotExist}}
		err := remotefs.PatchFile(f, "/new", []remotefs.Patch{
			remotefs.AppendIfMissing("hello"),
		}, remotefs.WithCreate(0o600))
		require.NoError(t, err)
		require.Equal(t, "hello\n", f.writtenStr())
		require.Equal(t, fs.FileMode(0o600), f.writtenPerm)
	})

	t.Run("strips type bits from WithCreate perm", func(t *testing.T) {
		f := &patchFS{statErr: &fs.PathError{Op: "stat", Path: "/new", Err: fs.ErrNotExist}}
		err := remotefs.PatchFile(f, "/new", []remotefs.Patch{
			remotefs.AppendIfMissing("hello"),
		}, remotefs.WithCreate(fs.ModeDir|0o755))
		require.NoError(t, err)
		require.Equal(t, fs.FileMode(0o755), f.writtenPerm, "type bits must be stripped from WithCreate perm")
	})

	t.Run("errors when missing without WithCreate", func(t *testing.T) {
		f := &patchFS{statErr: &fs.PathError{Op: "stat", Path: "/new", Err: fs.ErrNotExist}}
		err := remotefs.PatchFile(f, "/new", []remotefs.Patch{
			remotefs.AppendIfMissing("hello"),
		})
		require.Error(t, err)
		require.ErrorIs(t, err, fs.ErrNotExist)
	})
}

func TestPatchFileByRegex(t *testing.T) {
	t.Run("matches by regex", func(t *testing.T) {
		f := newPatchFS("foo=123\nbar=456\n")
		err := remotefs.PatchFile(f, "/etc/env", []remotefs.Patch{
			remotefs.ReplaceOrAppend(remotefs.ByRegex(`^foo=\d+$`), "foo=999"),
		})
		require.NoError(t, err)
		require.Equal(t, "foo=999\nbar=456\n", f.writtenStr())
	})

	t.Run("invalid regex surfaces as error", func(t *testing.T) {
		f := newPatchFS("foo=123\n")
		err := remotefs.PatchFile(f, "/etc/env", []remotefs.Patch{
			remotefs.ReplaceOrAppend(remotefs.ByRegex(`[invalid`), "foo=999"),
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid regex")
	})
}

func TestPatchFileTrailingNewline(t *testing.T) {
	t.Run("preserves absence of trailing newline", func(t *testing.T) {
		f := newPatchFS("line1\nline2")
		err := remotefs.PatchFile(f, "/etc/env", []remotefs.Patch{
			remotefs.AppendIfMissing("line3"),
		})
		require.NoError(t, err)
		require.Equal(t, "line1\nline2\nline3", f.writtenStr())
	})

	t.Run("preserves trailing newline when present", func(t *testing.T) {
		f := newPatchFS("line1\nline2\n")
		err := remotefs.PatchFile(f, "/etc/env", []remotefs.Patch{
			remotefs.AppendIfMissing("line3"),
		})
		require.NoError(t, err)
		require.Equal(t, "line1\nline2\nline3\n", f.writtenStr())
	})

	t.Run("existing empty file does not gain trailing newline", func(t *testing.T) {
		// An existing empty file is not treated as having a trailing newline;
		// only new files (via WithCreate) get one.
		f := newPatchFS("") // existing empty file, content = []byte{}
		err := remotefs.PatchFile(f, "/etc/env", []remotefs.Patch{
			remotefs.AppendIfMissing("hello"),
		})
		require.NoError(t, err)
		require.Equal(t, "hello", f.writtenStr())
	})

	t.Run("no-op patches do not add trailing newline", func(t *testing.T) {
		// A file without a trailing newline where the patch makes no change
		// must not gain a trailing newline — and must not be written at all.
		f := newPatchFS("line1\nline2")
		err := remotefs.PatchFile(f, "/etc/env", []remotefs.Patch{
			remotefs.AppendIfMissing("line1"), // already present — no-op
		})
		require.NoError(t, err)
		require.False(t, f.wasWritten(), "no-op patch should not write the file")
	})
}

func TestPatchFileNilApply(t *testing.T) {
	f := newPatchFS("line\n")
	err := remotefs.PatchFile(f, "/etc/env", []remotefs.Patch{{}})
	require.ErrorIs(t, err, remotefs.ErrNilPatch)
}

func TestPatchFileInvalidMatch(t *testing.T) {
	f := newPatchFS("line\n")
	// A zero-value LineMatch must produce ErrInvalidMatch, not delete lines.
	err := remotefs.PatchFile(f, "/etc/env", []remotefs.Patch{
		remotefs.DeleteMatching(remotefs.LineMatch{}),
	})
	require.ErrorIs(t, err, remotefs.ErrInvalidMatch)
	require.False(t, f.wasWritten())
}

func TestPatchFileMultilinePatch(t *testing.T) {
	f := newPatchFS("line\n")
	err := remotefs.PatchFile(f, "/etc/env", []remotefs.Patch{
		remotefs.AppendIfMissing("bad\nvalue"),
	})
	require.ErrorIs(t, err, remotefs.ErrMultilinePatch)
	require.False(t, f.wasWritten())
}

func TestPatchFileNotRegularFile(t *testing.T) {
	for _, mode := range []fs.FileMode{
		fs.ModeNamedPipe | 0o644,
		fs.ModeDevice | 0o644,
		fs.ModeDir | 0o755,
		fs.ModeSocket | 0o644,
	} {
		f := &patchFS{statMode: mode}
		err := remotefs.PatchFile(f, "/dev/stdin", []remotefs.Patch{
			remotefs.AppendIfMissing("line"),
		})
		require.ErrorIs(t, err, remotefs.ErrNotRegularFile, "mode %v should be rejected", mode)
	}
}

func TestPatchFileSymlinkAllowed(t *testing.T) {
	// Symlinks must not be rejected by the type check. BSD stat reports
	// ModeSymlink | 0o777 for symlinks; PatchFile should proceed but must use
	// 0o600 instead of the meaningless 0o777 symlink permission.
	f := &patchFS{
		content:  []byte("FOO=old\n"),
		statMode: fs.ModeSymlink | 0o777,
	}
	err := remotefs.PatchFile(f, "/etc/env", []remotefs.Patch{
		remotefs.ReplaceOrAppend(remotefs.ByPrefix("FOO="), "FOO=new"),
	})
	require.NoError(t, err)
	require.Equal(t, "FOO=new\n", f.writtenStr())
	require.Equal(t, fs.FileMode(0o600), f.writtenPerm, "symlink perm 0o777 must be replaced with safe 0o600")
}

func TestPatchFileNoWriteOnIdenticalContent(t *testing.T) {
	f := newPatchFS("FOO=bar\n")
	err := remotefs.PatchFile(f, "/etc/env", []remotefs.Patch{
		remotefs.AppendIfMissing("FOO=bar"), // already present — no-op
	})
	require.NoError(t, err)
	require.False(t, f.wasWritten(), "identical output must not trigger a write")
}

func TestPatchFileWithCreateEmptyOutput(t *testing.T) {
	// WithCreate on a missing file where patches produce empty output must still
	// create the file (bytes.Equal(nil, []byte{}) must not suppress the write).
	f := &patchFS{statErr: &fs.PathError{Op: "stat", Path: "/new", Err: fs.ErrNotExist}}
	err := remotefs.PatchFile(f, "/new", nil, remotefs.WithCreate(0o600))
	require.NoError(t, err)
	require.True(t, f.wasWritten(), "WithCreate must write even when output is empty")
}

func TestPatchFileStatError(t *testing.T) {
	f := &patchFS{statErr: errors.New("permission denied")}
	err := remotefs.PatchFile(f, "/etc/env", []remotefs.Patch{
		remotefs.AppendIfMissing("hello"),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "permission denied")
}

// TestPatchFilePosix verifies PatchFile works end-to-end via PosixFS.
func TestPatchFilePosix(t *testing.T) {
	mr := rigtest.NewMockRunner()
	// initStat probe: "stat --format=FORMAT" selects GNU stat (stat -c format).
	mr.AddCommandOutput(rigtest.Equal("stat --help 2>&1"), "Usage: stat --format=FORMAT")
	// PatchFile's Stat("/etc/env") — match path-specifically so the generic stat
	// handler below (for MkdirAll's Stat("/etc")) does not fire first.
	// 0x81a4 = 0o100644 (regular file, rw-r--r--).
	mr.AddCommandOutput(rigtest.Match(`stat -c.*etc/env`), "0x81a4 12 1234567890.000000000 ///etc/env//")
	// ReadFile via cat.
	mr.AddCommandOutput(rigtest.Contains("cat"), "FOO=old\nBAR=keep\n")
	// WriteFileAtomic: MkdirAll probes Stat("/etc") — return directory so no install -d is needed.
	// 0x41ed = 0o040755 (directory, rwxr-xr-x). This fires for any remaining stat -c call.
	mr.AddCommandOutput(rigtest.Contains("stat -c"), "0x41ed 0 1234567890.000000000 ///etc//")
	// CreateTemp.
	mr.AddCommandOutput(rigtest.Contains("mktemp"), "/etc/.tmp-abc123")
	// WriteFile via install -D + stdin.
	mr.AddCommandSuccess(rigtest.Contains("install"))
	// Chmod.
	mr.AddCommandSuccess(rigtest.Contains("chmod"))
	// Rename.
	mr.AddCommandSuccess(rigtest.Contains("mv -f"))

	f := remotefs.NewPosixFS(mr)
	err := remotefs.PatchFile(f, "/etc/env", []remotefs.Patch{
		remotefs.ReplaceOrAppend(remotefs.ByPrefix("FOO="), "FOO=new"),
	})
	require.NoError(t, err)
	require.NoError(t, mr.Received(rigtest.Contains("mktemp")))
	require.NoError(t, mr.Received(rigtest.Contains("mv -f")))
}
