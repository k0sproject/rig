package remotefs_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"testing"
	"time"

	"github.com/k0sproject/rig/v2/powershell"
	"github.com/k0sproject/rig/v2/remotefs"
	"github.com/k0sproject/rig/v2/rigtest"
	"github.com/stretchr/testify/require"
)

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

func TestWindowsChownVariantsNotSupported(t *testing.T) {
	mr := rigtest.NewMockRunner()
	mr.Windows = true
	f := remotefs.NewWindowsFS(mr)
	require.ErrorIs(t, f.ChownInt("/tmp/file", 1000, 2000), remotefs.ErrNotSupported)
	require.ErrorIs(t, f.ChownTree("/tmp", "root"), remotefs.ErrNotSupported)
	require.ErrorIs(t, f.ChownTreeInt("/tmp", 0, 0), remotefs.ErrNotSupported)
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

func TestWindowsCreateTemp(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		// Stub TempDir's TEMP lookup first (exact match), then the CreateTemp script (broad prefix).
		// The exact match on powershell.Cmd(...) distinguishes the two calls regardless of encoding.
		mr.AddCommandOutput(rigtest.Equal(powershell.Cmd("[System.Environment]::GetEnvironmentVariable('TEMP')")), `C:\Windows\Temp`)
		mr.AddCommandOutput(rigtest.HasPrefix("powershell.exe"), `C:\Windows\Temp\rig-abc123.tmp`)
		f := remotefs.NewWindowsFS(mr)
		path, err := f.CreateTemp("", "rig-")
		require.NoError(t, err)
		require.Equal(t, "C:/Windows/Temp/rig-abc123.tmp", path)
	})
}

func TestWindowsRename(t *testing.T) {
	const src = `C:\src\file.txt`
	const dst = `C:\dst\file.txt`
	// Move-Item uses double-quoted paths, which forces powershell.Cmd into
	// -EncodedCommand mode. Build the expected command the same way WinFS.Rename does.
	renameCmd := powershell.Cmd(fmt.Sprintf("Move-Item -Force -LiteralPath %s -Destination %s",
		powershell.DoubleQuotePath(src), powershell.DoubleQuotePath(dst)))

	t.Run("uses Force and LiteralPath", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandSuccess(rigtest.Equal(renameCmd))
		f := remotefs.NewWindowsFS(mr)
		require.NoError(t, f.Rename(src, dst))
		require.NoError(t, mr.Received(rigtest.Equal(renameCmd)))
	})

	t.Run("error includes both paths", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandFailure(rigtest.HasPrefix("powershell.exe"), errors.New("access denied"))
		f := remotefs.NewWindowsFS(mr)
		err := f.Rename(src, dst)
		require.Error(t, err)
		require.Contains(t, err.Error(), src)
		require.Contains(t, err.Error(), dst)
	})
}

func TestWinFSFollow(t *testing.T) {
	const path = `C:\logs\app.log`

	t.Run("output flows to writer", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommand(rigtest.Contains("powershell.exe"), func(a *rigtest.A) error {
			_, _ = a.Stdout.Write([]byte("new line\n"))
			return nil
		})
		fsys := remotefs.NewWindowsFS(mr)
		var buf bytes.Buffer
		require.NoError(t, fsys.Follow(context.Background(), path, &buf))
		require.Equal(t, "new line\n", buf.String())
	})

	t.Run("context cancellation returns nil", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		mr.AddCommand(rigtest.Contains("powershell.exe"), func(a *rigtest.A) error {
			return a.Ctx.Err()
		})
		fsys := remotefs.NewWindowsFS(mr)
		require.NoError(t, fsys.Follow(ctx, path, io.Discard), "context cancellation should not return an error")
	})

	t.Run("command error propagates", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandFailure(rigtest.Contains("powershell.exe"), errors.New("access denied"))
		fsys := remotefs.NewWindowsFS(mr)
		require.Error(t, fsys.Follow(context.Background(), path, io.Discard))
	})
}

func TestWinFSChmod(t *testing.T) {
	t.Run("writable mode clears read-only attribute", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandSuccess(rigtest.HasPrefix("powershell.exe"))
		fsys := remotefs.NewWindowsFS(mr)
		// 0o644 has the owner-write bit (0o200) set → should clear read-only.
		require.NoError(t, fsys.Chmod(`C:\file.txt`, 0o644))
		require.NoError(t, mr.Received(rigtest.HasPrefix("powershell.exe")))
		require.NoError(t, mr.NotReceived(rigtest.Contains("attrib")))
		// Decode the -EncodedCommand payload and verify the bitwise-clear operation.
		script, ok := decodePSScript(mr.LastCommand())
		require.True(t, ok, "Chmod should use an encoded PS command to prevent $a expansion by an outer PS host")
		require.Contains(t, script, "Get-Item")
		require.Contains(t, script, "-band -bnot [IO.FileAttributes]::ReadOnly")
	})

	t.Run("read-only mode sets read-only attribute", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandSuccess(rigtest.HasPrefix("powershell.exe"))
		fsys := remotefs.NewWindowsFS(mr)
		// 0o444 has no owner-write bit → should set read-only.
		require.NoError(t, fsys.Chmod(`C:\file.txt`, fs.FileMode(0o444)))
		require.NoError(t, mr.Received(rigtest.HasPrefix("powershell.exe")))
		require.NoError(t, mr.NotReceived(rigtest.Contains("attrib")))
		// Decode the -EncodedCommand payload and verify the bitwise-set operation.
		script, ok := decodePSScript(mr.LastCommand())
		require.True(t, ok, "Chmod should use an encoded PS command to prevent $a expansion by an outer PS host")
		require.Contains(t, script, "Get-Item")
		require.Contains(t, script, "-bor [IO.FileAttributes]::ReadOnly")
	})
}

