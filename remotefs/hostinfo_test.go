package remotefs_test

import (
	"context"
	"encoding/base64"
	"errors"
	"io/fs"
	"testing"
	"time"

	"github.com/k0sproject/rig/v2/remotefs"
	"github.com/k0sproject/rig/v2/rigtest"
	"github.com/stretchr/testify/require"
)

func TestPosixMachineID(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandOutput(rigtest.Equal("cat /etc/machine-id"), "abc123def456")
		fs := remotefs.NewPosixFS(mr)
		id, err := fs.MachineID()
		require.NoError(t, err)
		require.Equal(t, "abc123def456", id)
	})

	t.Run("empty", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandOutput(rigtest.Equal("cat /etc/machine-id"), "")
		fs := remotefs.NewPosixFS(mr)
		_, err := fs.MachineID()
		require.ErrorIs(t, err, remotefs.ErrEmptyMachineID)
	})

	t.Run("command fails", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandFailure(rigtest.Equal("cat /etc/machine-id"), errors.New("no such file"))
		fs := remotefs.NewPosixFS(mr)
		_, err := fs.MachineID()
		require.Error(t, err)
	})
}

func TestPosixSystemTime(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandOutput(rigtest.Equal("date -u +%s"), "1700000000")
		fs := remotefs.NewPosixFS(mr)
		got, err := fs.SystemTime()
		require.NoError(t, err)
		require.Equal(t, time.Unix(1700000000, 0), got)
	})

	t.Run("invalid output", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandOutput(rigtest.Equal("date -u +%s"), "not-a-number")
		fs := remotefs.NewPosixFS(mr)
		_, err := fs.SystemTime()
		require.Error(t, err)
	})
}

func TestWindowsMachineID(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandOutput(rigtest.HasPrefix("powershell.exe"), "6ba7b810-9dad-11d1-80b4-00c04fd430c8")
		fs := remotefs.NewWindowsFS(mr)
		id, err := fs.MachineID()
		require.NoError(t, err)
		require.Equal(t, "6ba7b810-9dad-11d1-80b4-00c04fd430c8", id)
	})

	t.Run("empty", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandOutput(rigtest.HasPrefix("powershell.exe"), "")
		fs := remotefs.NewWindowsFS(mr)
		_, err := fs.MachineID()
		require.ErrorIs(t, err, remotefs.ErrEmptyMachineID)
	})
}

func TestWindowsSystemTime(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandOutput(rigtest.HasPrefix("powershell.exe"), "1700000000")
		fs := remotefs.NewWindowsFS(mr)
		got, err := fs.SystemTime()
		require.NoError(t, err)
		require.Equal(t, time.Unix(1700000000, 0), got)
	})

	t.Run("invalid output", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandOutput(rigtest.HasPrefix("powershell.exe"), "not-a-number")
		fs := remotefs.NewWindowsFS(mr)
		_, err := fs.SystemTime()
		require.Error(t, err)
	})
}

// PosixFS

func TestPosixDownloadURL(t *testing.T) {
	t.Run("curl", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandOutput(rigtest.Equal("command -v curl"), "/usr/bin/curl")
		mr.AddCommandSuccess(rigtest.HasPrefix("curl"))
		f := remotefs.NewPosixFS(mr)
		require.NoError(t, f.DownloadURL("http://example.com/file", "/tmp/file"))
	})

	t.Run("wget fallback", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandFailure(rigtest.Equal("command -v curl"), errors.New("not found"))
		mr.AddCommandOutput(rigtest.Equal("command -v wget"), "/usr/bin/wget")
		mr.AddCommandSuccess(rigtest.HasPrefix("wget"))
		f := remotefs.NewPosixFS(mr)
		require.NoError(t, f.DownloadURL("http://example.com/file", "/tmp/file"))
	})

	t.Run("neither available", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandFailure(rigtest.Equal("command -v curl"), errors.New("not found"))
		mr.AddCommandFailure(rigtest.Equal("command -v wget"), errors.New("not found"))
		f := remotefs.NewPosixFS(mr)
		err := f.DownloadURL("http://example.com/file", "/tmp/file")
		require.Error(t, err)
	})
}

func TestPosixFileContains(t *testing.T) {
	t.Run("match", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandOutput(rigtest.Contains("grep -qF"), "0")
		f := remotefs.NewPosixFS(mr)
		ok, err := f.FileContains("/tmp/file", "needle")
		require.NoError(t, err)
		require.True(t, ok)
	})

	t.Run("no match", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandOutput(rigtest.Contains("grep -qF"), "1")
		f := remotefs.NewPosixFS(mr)
		ok, err := f.FileContains("/tmp/file", "needle")
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("file not exist", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandOutput(rigtest.Contains("grep -qF"), "2")
		mr.AddCommandOutput(rigtest.Contains("test -e"), "1")
		f := remotefs.NewPosixFS(mr)
		ok, err := f.FileContains("/tmp/file", "needle")
		require.ErrorIs(t, err, fs.ErrNotExist)
		require.False(t, ok)
	})

	t.Run("read error", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandOutput(rigtest.Contains("grep -qF"), "2")
		mr.AddCommandOutput(rigtest.Contains("test -e"), "0")
		f := remotefs.NewPosixFS(mr)
		ok, err := f.FileContains("/tmp/file", "needle")
		require.Error(t, err)
		require.False(t, ok)
	})
}

func TestPosixIsContainer(t *testing.T) {
	t.Run("dockerenv", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("/.dockerenv"))
		f := remotefs.NewPosixFS(mr)
		ok, err := f.IsContainer()
		require.NoError(t, err)
		require.True(t, ok)
	})

	t.Run("containerenv", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandFailure(rigtest.Contains("/.dockerenv"), errors.New("not found"))
		mr.AddCommandSuccess(rigtest.Contains(".containerenv"))
		f := remotefs.NewPosixFS(mr)
		ok, err := f.IsContainer()
		require.NoError(t, err)
		require.True(t, ok)
	})

	t.Run("cgroup docker", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandFailure(rigtest.Contains("/.dockerenv"), errors.New("not found"))
		mr.AddCommandFailure(rigtest.Contains(".containerenv"), errors.New("not found"))
		mr.AddCommandOutput(rigtest.Contains("/proc/1/cgroup"), "12:devices:/docker/abc123")
		f := remotefs.NewPosixFS(mr)
		ok, err := f.IsContainer()
		require.NoError(t, err)
		require.True(t, ok)
	})

	t.Run("not container", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandFailure(rigtest.Contains("/.dockerenv"), errors.New("not found"))
		mr.AddCommandFailure(rigtest.Contains(".containerenv"), errors.New("not found"))
		mr.AddCommandOutput(rigtest.Contains("/proc/1/cgroup"), "11:devices:/init.scope")
		f := remotefs.NewPosixFS(mr)
		ok, err := f.IsContainer()
		require.NoError(t, err)
		require.False(t, ok)
	})
}

func TestPosixTouch(t *testing.T) {
	t.Run("no timestamp", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.HasPrefix("touch"))
		f := remotefs.NewPosixFS(mr)
		require.NoError(t, f.Touch("/tmp/file"))
	})

	t.Run("with timestamp", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		// Initial touch creates/updates the file.
		mr.AddCommandSuccess(rigtest.HasPrefix("touch -- "))
		// initTouch probes touch --help; returning "BusyBox" triggers secChtimes
		// (the simpler path that sets atime/mtime individually without creating a
		// temp file, which would require mocking a complex stat/create/remove chain).
		mr.AddCommandOutput(rigtest.Equal("touch --help 2>&1"), "BusyBox v1.35")
		// secChtimes issues two touch commands: one for atime (-a) and one for mtime (-m).
		mr.AddCommandSuccess(rigtest.Contains("TZ=UTC touch"))
		f := remotefs.NewPosixFS(mr)
		ts := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
		require.NoError(t, f.Touch("/tmp/file", ts))
	})
}

// WinFS — PS commands are base64-encoded so we match by powershell.exe prefix.

func TestWindowsDownloadURL(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandSuccess(rigtest.HasPrefix("powershell.exe"))
		f := remotefs.NewWindowsFS(mr)
		require.NoError(t, f.DownloadURL("http://example.com/file", `C:\tmp\file`))
	})

	t.Run("failure", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandFailure(rigtest.HasPrefix("powershell.exe"), errors.New("exit 1"))
		f := remotefs.NewWindowsFS(mr)
		err := f.DownloadURL("http://example.com/file", `C:\tmp\file`)
		require.Error(t, err)
	})
}

func TestWindowsFileContains(t *testing.T) {
	t.Run("match", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandOutput(rigtest.HasPrefix("powershell.exe"), "MATCH")
		f := remotefs.NewWindowsFS(mr)
		ok, err := f.FileContains(`C:\tmp\file`, "needle")
		require.NoError(t, err)
		require.True(t, ok)
	})

	t.Run("no match", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandOutput(rigtest.HasPrefix("powershell.exe"), "NO_MATCH")
		f := remotefs.NewWindowsFS(mr)
		ok, err := f.FileContains(`C:\tmp\file`, "needle")
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("not found", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandOutput(rigtest.HasPrefix("powershell.exe"), "NOT_FOUND")
		f := remotefs.NewWindowsFS(mr)
		ok, err := f.FileContains(`C:\tmp\file`, "needle")
		require.ErrorIs(t, err, fs.ErrNotExist)
		require.False(t, ok)
	})

	t.Run("script error", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandOutput(rigtest.HasPrefix("powershell.exe"), "ERROR:access denied")
		f := remotefs.NewWindowsFS(mr)
		ok, err := f.FileContains(`C:\tmp\file`, "needle")
		require.Error(t, err)
		require.False(t, ok)
	})
}

func TestWindowsTouch(t *testing.T) {
	t.Run("no timestamp", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandSuccess(rigtest.HasPrefix("powershell.exe"))
		f := remotefs.NewWindowsFS(mr)
		require.NoError(t, f.Touch(`C:\tmp\file`))
	})

	t.Run("with timestamp", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandSuccess(rigtest.HasPrefix("powershell.exe"))
		f := remotefs.NewWindowsFS(mr)
		ts := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
		require.NoError(t, f.Touch(`C:\tmp\file`, ts))
	})
}

func TestWindowsIsContainer(t *testing.T) {
	mr := rigtest.NewMockRunner()
	mr.Windows = true
	f := remotefs.NewWindowsFS(mr)
	ok, err := f.IsContainer()
	require.ErrorIs(t, err, remotefs.ErrNotSupported)
	require.False(t, ok)
}

func TestPosixDir(t *testing.T) {
	f := remotefs.NewPosixFS(rigtest.NewMockRunner())
	require.Equal(t, "/foo/bar", f.Dir("/foo/bar/baz"))
	require.Equal(t, "/foo", f.Dir("/foo/bar"))
	require.Equal(t, "/", f.Dir("/foo"))
	require.Equal(t, ".", f.Dir("foo"))
	require.Equal(t, ".", f.Dir(""))
}

func TestPosixBase(t *testing.T) {
	f := remotefs.NewPosixFS(rigtest.NewMockRunner())
	require.Equal(t, "baz", f.Base("/foo/bar/baz"))
	require.Equal(t, "bar", f.Base("/foo/bar"))
	require.Equal(t, "foo", f.Base("/foo"))
	require.Equal(t, "foo", f.Base("foo"))
}

func TestPosixCommandExist(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandOutput(rigtest.Equal("command -v curl"), "/usr/bin/curl")
		f := remotefs.NewPosixFS(mr)
		require.True(t, f.CommandExist("curl"))
	})
	t.Run("not found", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandFailure(rigtest.Equal("command -v curl"), errors.New("not found"))
		f := remotefs.NewPosixFS(mr)
		require.False(t, f.CommandExist("curl"))
	})
}

func TestWindowsDir(t *testing.T) {
	mr := rigtest.NewMockRunner()
	mr.Windows = true
	f := remotefs.NewWindowsFS(mr)
	require.Equal(t, `C:\foo\bar`, f.Dir(`C:\foo\bar\baz`))
	require.Equal(t, `C:\foo`, f.Dir(`C:\foo\bar`))
	require.Equal(t, `C:\`, f.Dir(`C:\foo`))
	require.Equal(t, `C:\`, f.Dir(`C:\`))
	require.Equal(t, ".", f.Dir("foo"))
	require.Equal(t, ".", f.Dir(""))
	require.Equal(t, `\`, f.Dir(`\`))
	require.Equal(t, `/`, f.Dir(`/`))
	// forward slashes preserved
	require.Equal(t, "C:/foo", f.Dir("C:/foo/bar"))
	require.Equal(t, "C:/", f.Dir("C:/foo"))
	require.Equal(t, "C:/", f.Dir("C:/"))
}

func TestWindowsBase(t *testing.T) {
	mr := rigtest.NewMockRunner()
	mr.Windows = true
	f := remotefs.NewWindowsFS(mr)
	require.Equal(t, "baz", f.Base(`C:\foo\bar\baz`))
	require.Equal(t, "bar", f.Base(`C:\foo\bar`))
	require.Equal(t, "foo", f.Base(`C:\foo`))
	require.Equal(t, "foo", f.Base("foo"))
	require.Equal(t, ".", f.Base(""))
	require.Equal(t, `\`, f.Base(`\`))
	require.Equal(t, `\`, f.Base(`\\`))
	require.Equal(t, `/`, f.Base(`/`))
	// drive roots
	require.Equal(t, `C:\`, f.Base(`C:\`))
	require.Equal(t, `C:/`, f.Base(`C:/`))
}

func TestWindowsCommandExist(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandOutput(rigtest.HasPrefix("powershell.exe"), `C:\Windows\System32\curl.exe`)
		f := remotefs.NewWindowsFS(mr)
		require.True(t, f.CommandExist("curl"))
	})
	t.Run("not found via error", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandFailure(rigtest.HasPrefix("powershell.exe"), errors.New("not found"))
		f := remotefs.NewWindowsFS(mr)
		require.False(t, f.CommandExist("curl"))
	})
	t.Run("not found via empty output", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandOutput(rigtest.HasPrefix("powershell.exe"), "")
		f := remotefs.NewWindowsFS(mr)
		require.False(t, f.CommandExist("curl"))
	})
}

func TestPosixChownInt(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("chown -- 1000:2000"))
		f := remotefs.NewPosixFS(mr)
		require.NoError(t, f.ChownInt("/tmp/file", 1000, 2000))
	})
	t.Run("not exist", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandFailure(rigtest.Contains("chown -- 1000:2000"), errors.New("No such file or directory"))
		f := remotefs.NewPosixFS(mr)
		require.ErrorIs(t, f.ChownInt("/tmp/file", 1000, 2000), fs.ErrNotExist)
	})
}

func TestPosixChownTree(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("chown -R -- root:root"))
		f := remotefs.NewPosixFS(mr)
		require.NoError(t, f.ChownTree("/srv", "root:root"))
	})
	t.Run("not exist", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandFailure(rigtest.Contains("chown -R -- root:root"), errors.New("No such file or directory"))
		f := remotefs.NewPosixFS(mr)
		require.ErrorIs(t, f.ChownTree("/srv", "root:root"), fs.ErrNotExist)
	})
}

func TestPosixChownTreeInt(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("chown -R -- 0:0"))
		f := remotefs.NewPosixFS(mr)
		require.NoError(t, f.ChownTreeInt("/srv", 0, 0))
	})
	t.Run("not exist", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandFailure(rigtest.Contains("chown -R -- 0:0"), errors.New("No such file or directory"))
		f := remotefs.NewPosixFS(mr)
		require.ErrorIs(t, f.ChownTreeInt("/srv", 0, 0), fs.ErrNotExist)
	})
}

func TestWindowsChownVariantsNotSupported(t *testing.T) {
	mr := rigtest.NewMockRunner()
	mr.Windows = true
	f := remotefs.NewWindowsFS(mr)
	require.ErrorIs(t, f.ChownInt("/tmp/file", 1000, 2000), remotefs.ErrNotSupported)
	require.ErrorIs(t, f.ChownTree("/tmp", "root"), remotefs.ErrNotSupported)
	require.ErrorIs(t, f.ChownTreeInt("/tmp", 0, 0), remotefs.ErrNotSupported)
}

func TestPosixHTTPStatus(t *testing.T) {
	t.Run("200", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandOutput(rigtest.Equal("command -v curl"), "/usr/bin/curl")
		resp200 := base64.StdEncoding.EncodeToString([]byte("HTTP/1.1 200 OK\r\n\r\n"))
		mr.AddCommandOutput(rigtest.Contains("--http1.1"), resp200)
		f := remotefs.NewPosixFS(mr)
		code, err := remotefs.HTTPStatus(context.Background(), f, "http://example.com/health")
		require.NoError(t, err)
		require.Equal(t, 200, code)
	})
	t.Run("503", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandOutput(rigtest.Equal("command -v curl"), "/usr/bin/curl")
		resp503 := base64.StdEncoding.EncodeToString([]byte("HTTP/1.1 503 Service Unavailable\r\n\r\n"))
		mr.AddCommandOutput(rigtest.Contains("--http1.1"), resp503)
		f := remotefs.NewPosixFS(mr)
		code, err := remotefs.HTTPStatus(context.Background(), f, "http://example.com/health")
		require.NoError(t, err)
		require.Equal(t, 503, code)
	})
	t.Run("curl unavailable", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandFailure(rigtest.Equal("command -v curl"), errors.New("not found"))
		f := remotefs.NewPosixFS(mr)
		_, err := remotefs.HTTPStatus(context.Background(), f, "http://example.com/health")
		require.Error(t, err)
	})
}

func TestWindowsHTTPStatus(t *testing.T) {
	t.Run("200", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		resp200 := base64.StdEncoding.EncodeToString([]byte("HTTP/1.1 200 OK\r\n\r\n"))
		mr.AddCommandOutput(rigtest.HasPrefix("powershell.exe"), resp200)
		f := remotefs.NewWindowsFS(mr)
		code, err := remotefs.HTTPStatus(context.Background(), f, "http://example.com/health")
		require.NoError(t, err)
		require.Equal(t, 200, code)
	})
	t.Run("failure", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandFailure(rigtest.HasPrefix("powershell.exe"), errors.New("exit 1"))
		f := remotefs.NewWindowsFS(mr)
		_, err := remotefs.HTTPStatus(context.Background(), f, "http://example.com/health")
		require.Error(t, err)
	})
}
