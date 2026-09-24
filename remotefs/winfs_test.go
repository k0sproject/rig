package remotefs_test

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/iotest"
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
	// CreateTemp names the file the transfer writes to, so its output has to be
	// a path: an empty one used to pass this test while running Invoke-WebRequest
	// with -OutFile "".
	const downloadTmp = `C:\tmp\file.AbCdEf`

	t.Run("ok", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandOutput(rigtest.HasPrefix("powershell.exe"), downloadTmp)
		f := remotefs.NewWindowsFS(mr)
		require.NoError(t, f.DownloadURL("http://test.invalid/file", `C:\tmp\file`))

		script, ok := decodePSScript(mr.LastCommand())
		require.True(t, ok, "expected an encoded script, got %q", mr.LastCommand())
		require.Contains(t, script, `Move-Item -Force -LiteralPath "`+downloadTmp+`"`,
			"the temporary must be renamed into place")
	})

	t.Run("failure", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandFailure(rigtest.HasPrefix("powershell.exe"), errors.New("exit 1"))
		f := remotefs.NewWindowsFS(mr)
		err := f.DownloadURL("http://test.invalid/file", `C:\tmp\file`)
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

func TestWindowsNativePath(t *testing.T) {
	mr := rigtest.NewMockRunner()
	mr.Windows = true
	fs := remotefs.NewWindowsFS(mr)

	cases := []struct {
		input string
		want  string
	}{
		{"foo/bar/baz", `foo\bar\baz`},
		{`already\windows`, `already\windows`},
		{"no-slashes", "no-slashes"},
		{"", ""},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, fs.NativePath(tc.input))
	}
}

func TestWindowsShellQuote(t *testing.T) {
	mr := rigtest.NewMockRunner()
	mr.Windows = true
	fs := remotefs.NewWindowsFS(mr)

	cases := []struct {
		input string
		want  string
	}{
		{"hello", "'hello'"},
		{"hello world", "'hello world'"},
		{"say 'it'", "'say `'it`''"},
		{"$var", "'$var'"},
		{"$(evil)", "'$(evil)'"},
		{"back`tick", "'back``tick'"},
		{"", "''"},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, fs.ShellQuote(tc.input), "input: %q", tc.input)
	}
}

func TestWindowsStat(t *testing.T) {
	// A missing path is reported by a *successful* stat command that prints the
	// {"Err":"does not exist"} marker, so an execution failure is never evidence
	// that the path is absent.
	const statOutput = `{"Err":"does not exist"}`

	t.Run("missing file is ErrNotExist", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandOutput(rigtest.HasPrefix("powershell.exe"), statOutput)

		_, err := remotefs.NewWindowsFS(mr).Stat("C:\\app\\missing.conf")
		require.ErrorIs(t, err, fs.ErrNotExist)
	})

	for _, tc := range []struct {
		name     string
		failWith error
	}{
		{"not connected", errors.New("start command: runner start command: not connected")},
		{"connection dropped", fmt.Errorf("create shell: %w", io.EOF)},
		{"command failed on the host", errors.New("access is denied")},
	} {
		t.Run("execution failure: "+tc.name, func(t *testing.T) {
			mr := rigtest.NewMockRunner()
			mr.Windows = true
			mr.AddCommandFailure(rigtest.HasPrefix("powershell.exe"), tc.failWith)

			_, err := remotefs.NewWindowsFS(mr).Stat("C:\\app\\existing.conf")
			require.Error(t, err)
			require.NotErrorIs(t, err, fs.ErrNotExist,
				"a failure to run the stat command is not evidence that the path is absent")
			require.ErrorIs(t, err, tc.failWith, "the original cause must stay reachable")
		})
	}
}

// TestWindowsPatchFileStatFailure is the data-loss regression guard: told the
// file is absent, PatchFile with WithCreate rebuilds it from an empty base and
// renames the result over content it never read.
func TestWindowsPatchFileStatFailure(t *testing.T) {
	connLost := errors.New("start command: runner start command: not connected")

	mr := rigtest.NewMockRunner()
	mr.Windows = true
	mr.AddCommandFailure(rigtest.HasPrefix("powershell.exe"), connLost)

	err := remotefs.PatchFile(remotefs.NewWindowsFS(mr), "C:\\app\\existing.conf", []remotefs.Patch{
		remotefs.AppendIfMissing("new_setting = 1"),
	}, remotefs.WithCreate(0o644))

	require.Error(t, err)
	require.ErrorIs(t, err, connLost)
	require.NotErrorIs(t, err, fs.ErrNotExist)
	// The decisive assertion: the failed stat is the only command sent. Checking
	// the returned error alone would also pass against the unfixed code, which
	// fails later on the write it should never have attempted.
	require.Equal(t, 1, mr.Len(), "nothing may be attempted after a stat that never ran: %v", mr.Commands())
}

// statJSON is the stat output for an existing path. mode is the PowerShell Mode
// string, whose leading "d" marks a directory.
func statJSON(name, mode string) string {
	return fmt.Sprintf(
		`{"Name":%q,"FullName":%q,"Mode":%q,"Length":0,"IsReadOnly":false,"LastWriteTime":"\/Date(1700000000000)\/"}`,
		name, name, mode,
	)
}

const statMissingJSON = `{"Err":"does not exist"}`

func TestWindowsRemove(t *testing.T) {
	// Remove must never decide "not a directory" from a stat it could not
	// perform, and never fall through to del on a stat failure.
	const name = `C:\app\tree`

	newRunner := func(statOut string, statErr error) *rigtest.MockRunner {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		if statErr != nil {
			mr.AddCommandFailure(rigtest.HasPrefix("powershell.exe"), statErr)
		} else {
			mr.AddCommandOutput(rigtest.HasPrefix("powershell.exe"), statOut)
		}
		return mr
	}

	t.Run("existing file", func(t *testing.T) {
		mr := newRunner(statJSON(name, "-a----"), nil)
		mr.AddCommandSuccess(rigtest.Contains("del"))
		require.NoError(t, remotefs.NewWindowsFS(mr).Remove(name))
		require.NoError(t, mr.Received(rigtest.Contains(`del "C:\app\tree"`)))
	})

	t.Run("existing directory", func(t *testing.T) {
		mr := newRunner(statJSON(name, "d-----"), nil)
		mr.AddCommandSuccess(rigtest.Contains("rmdir"))
		require.NoError(t, remotefs.NewWindowsFS(mr).Remove(name))
		require.NoError(t, mr.Received(rigtest.Contains(`rmdir /q "C:\app\tree"`)))
	})

	t.Run("missing path is an error", func(t *testing.T) {
		mr := newRunner(statMissingJSON, nil)
		err := remotefs.NewWindowsFS(mr).Remove(name)
		require.ErrorIs(t, err, fs.ErrNotExist, "os.Remove errors on a missing path")
		requirePathErrorOp(t, err, remotefs.OpRemove)
		require.NoError(t, mr.NotReceived(rigtest.Contains("del")))
	})

	t.Run("stat transport failure", func(t *testing.T) {
		connLost := errors.New("start command: runner start command: not connected")
		mr := newRunner("", connLost)
		err := remotefs.NewWindowsFS(mr).Remove(name)
		require.ErrorIs(t, err, connLost, "the transport cause must stay reachable")
		require.NotErrorIs(t, err, fs.ErrNotExist)
		requirePathErrorOp(t, err, remotefs.OpRemove)
		require.NoError(t, mr.NotReceived(rigtest.Contains("del")))
		require.NoError(t, mr.NotReceived(rigtest.Contains("rmdir")))
	})

	t.Run("delete command failure", func(t *testing.T) {
		delFailed := errors.New("exit code 1")
		mr := newRunner(statJSON(name, "-a----"), nil)
		mr.AddCommandFailure(rigtest.Contains("del"), delFailed)
		err := remotefs.NewWindowsFS(mr).Remove(name)
		require.ErrorIs(t, err, delFailed)
		requirePathErrorOp(t, err, remotefs.OpRemove)
	})
}

func TestWindowsRemoveAll(t *testing.T) {
	// RemoveAll must reach the recursive delete whenever the path really is a
	// directory, and must never fall back to the non-recursive rmdir -- or return
	// success -- because the stat could not be performed.
	const name = `C:\app\tree`

	newRunner := func(statOut string, statErr error) *rigtest.MockRunner {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		if statErr != nil {
			mr.AddCommandFailure(rigtest.HasPrefix("powershell.exe"), statErr)
		} else {
			mr.AddCommandOutput(rigtest.HasPrefix("powershell.exe"), statOut)
		}
		return mr
	}

	t.Run("existing file", func(t *testing.T) {
		mr := newRunner(statJSON(name, "-a----"), nil)
		mr.AddCommandSuccess(rigtest.Contains("del"))
		require.NoError(t, remotefs.NewWindowsFS(mr).RemoveAll(name))
		require.NoError(t, mr.Received(rigtest.Contains(`del "C:\app\tree"`)))
	})

	t.Run("populated directory", func(t *testing.T) {
		mr := newRunner(statJSON(name, "d-----"), nil)
		mr.AddCommandSuccess(rigtest.Contains("rmdir"))
		require.NoError(t, remotefs.NewWindowsFS(mr).RemoveAll(name))
		require.NoError(t, mr.Received(rigtest.Contains(`rmdir /s /q "C:\app\tree"`)))
	})

	t.Run("missing path is not an error", func(t *testing.T) {
		mr := newRunner(statMissingJSON, nil)
		require.NoError(t, remotefs.NewWindowsFS(mr).RemoveAll(name), "os.RemoveAll accepts a missing path")
		require.NoError(t, mr.NotReceived(rigtest.Contains("del")))
		require.NoError(t, mr.NotReceived(rigtest.Contains("rmdir")))
	})

	t.Run("stat transport failure", func(t *testing.T) {
		connLost := errors.New("start command: runner start command: not connected")
		mr := newRunner("", connLost)
		err := remotefs.NewWindowsFS(mr).RemoveAll(name)
		require.ErrorIs(t, err, connLost, "the transport cause must stay reachable")
		require.NotErrorIs(t, err, fs.ErrNotExist,
			"a stat that never ran must not read as 'nothing to remove'")
		requirePathErrorOp(t, err, remotefs.OpRemoveAll)
		// The regression guard: the tree survived and the caller was told a
		// non-recursive rmdir found it non-empty, never mentioning the connection.
		require.NoError(t, mr.NotReceived(rigtest.Contains("rmdir")))
		require.NoError(t, mr.NotReceived(rigtest.Contains("del")))
	})

	t.Run("recursive delete failure", func(t *testing.T) {
		rmdirFailed := errors.New("exit code 145")
		mr := newRunner(statJSON(name, "d-----"), nil)
		mr.AddCommandFailure(rigtest.Contains("rmdir"), rmdirFailed)
		err := remotefs.NewWindowsFS(mr).RemoveAll(name)
		require.ErrorIs(t, err, rmdirFailed)
		requirePathErrorOp(t, err, remotefs.OpRemoveAll)
	})
}

// fakeRigrcp stands in for the rigrcp helper on a Windows host, serving
// content as the file it has open, and records every command it receives.
type fakeRigrcp struct {
	mu       sync.Mutex
	content  []byte
	commands []string
	// writeErr, when set, is reported as the outcome of every write, which
	// then leaves content alone.
	writeErr string
	// rejectWrite, when set, is the reply to every w command, before any
	// payload is sent.
	rejectWrite string
}

// isRigrcp matches the command that starts the rigrcp helper, telling it
// apart from the one-shot PowerShell commands WinFS also runs.
func isRigrcp(command string) bool {
	script, ok := decodePSScript(command)
	return ok && strings.Contains(script, "GZipStream")
}

func (r *fakeRigrcp) handle(a *rigtest.A) error {
	in := bufio.NewReader(a.Stdin)
	for {
		line, err := in.ReadString('\n')
		if err != nil {
			return nil //nolint:nilerr // stdin closing is how a session ends
		}
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == "q" {
			r.record(line)
			return nil
		}
		resp, payload := r.respond(line)
		if _, err := a.Stdout.Write(append([]byte(resp), 0)); err != nil {
			return fmt.Errorf("write response: %w", err)
		}
		if len(fields) > 0 && fields[0] == "w" && !strings.Contains(resp, "error") && r.writeErr != "" {
			count, _, err := countAndPos(fields)
			if err != nil {
				return err
			}
			if _, err := io.CopyN(io.Discard, in, int64(count)); err != nil {
				return fmt.Errorf("discard payload: %w", err)
			}
			if _, err := fmt.Fprintf(a.Stdout, `{"error":%q}`+"\x00", r.writeErr); err != nil {
				return fmt.Errorf("write completion: %w", err)
			}
			continue
		}
		if len(fields) > 0 && fields[0] == "w" && !strings.Contains(resp, "error") {
			count, err := r.receive(in, fields)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(a.Stdout, `{"n":%d}`+"\x00", count); err != nil {
				return fmt.Errorf("write completion: %w", err)
			}
			continue
		}
		// A zero-length write to an io.Pipe still waits for a reader.
		if len(payload) == 0 {
			continue
		}
		if _, err := a.Stdout.Write(payload); err != nil {
			return fmt.Errorf("write payload: %w", err)
		}
	}
}

func (r *fakeRigrcp) record(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commands = append(r.commands, strings.TrimSuffix(line, "\n"))
}

// countAndPos parses the "<count> <pos>" arguments of r and w.
func countAndPos(fields []string) (int, int, error) {
	if len(fields) != 3 {
		return 0, 0, fmt.Errorf("want count and position, got %q", fields[1:])
	}
	count, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, fmt.Errorf("count: %w", err)
	}
	pos, err := strconv.Atoi(fields[2])
	if err != nil {
		return 0, 0, fmt.Errorf("position: %w", err)
	}
	return count, pos, nil
}

func (r *fakeRigrcp) respond(line string) (string, []byte) {
	r.record(line)
	r.mu.Lock()
	defer r.mu.Unlock()

	fields := strings.Fields(line)
	if len(fields) == 0 {
		return `{"error":"invalid command"}`, nil
	}
	switch fields[0] {
	case "o":
		if len(fields) < 2 {
			return `{"error":"missing mode"}`, nil
		}
		switch fields[1] {
		case "Create", "Truncate":
			r.content = r.content[:0]
		case "CreateNew":
			return `{"error":"The file already exists."}`, nil
		}
		return fmt.Sprintf(`{"pos":0,"size":%d}`, len(r.content)), nil
	case "r":
		count, pos, err := countAndPos(fields)
		if err != nil {
			return fmt.Sprintf(`{"error":%q}`, err.Error()), nil
		}
		remaining := r.content[min(pos, len(r.content)):]
		if count == -1 {
			return fmt.Sprintf(`{"n":%d}`, len(remaining)), remaining
		}
		if len(remaining) == 0 {
			return `{"error":"eof"}`, nil
		}
		chunk := remaining[:min(count, len(remaining))]
		return fmt.Sprintf(`{"n":%d}`, len(chunk)), chunk
	case "w":
		if r.rejectWrite != "" {
			return fmt.Sprintf(`{"error":%q}`, r.rejectWrite), nil
		}
		count, _, err := countAndPos(fields)
		if err != nil {
			return fmt.Sprintf(`{"error":%q}`, err.Error()), nil
		}
		return fmt.Sprintf(`{"n":%d}`, count), nil
	case "c":
		return `{"pos":-1}`, nil
	default:
		return `{"error":"invalid command"}`, nil
	}
}

// receive reads the payload of a w command and writes it into content at
// the position the command named, returning how much it wrote.
func (r *fakeRigrcp) receive(in io.Reader, fields []string) (int, error) {
	count, pos, err := countAndPos(fields)
	if err != nil {
		return 0, err
	}
	payload := make([]byte, count)
	if _, err := io.ReadFull(in, payload); err != nil {
		return 0, fmt.Errorf("read payload: %w", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if end := pos + count; end > len(r.content) {
		r.content = append(r.content, make([]byte, end-len(r.content))...)
	}
	copy(r.content[pos:], payload)
	return count, nil
}

func (r *fakeRigrcp) received() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.commands...)
}

func (r *fakeRigrcp) file() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]byte(nil), r.content...)
}

// newRigrcpRunner returns a runner whose rigrcp helper serves content, and
// whose stat reports an existing file.
func newRigrcpRunner(content []byte) (*rigtest.MockRunner, *fakeRigrcp) {
	rcp := &fakeRigrcp{content: content}
	mr := rigtest.NewMockRunner()
	mr.Windows = true
	mr.ErrDefault = errors.New("unexpected command")
	mr.AddCommand(isRigrcp, rcp.handle)
	mr.AddCommandOutput(rigtest.HasPrefix("powershell.exe"), statJSON(`C:\app\file.txt`, "-a----"))
	return mr, rcp
}

func TestWindowsReadFile(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content []byte
	}{
		{"empty", []byte{}},
		{"small", []byte("hello")},
		{"larger than a read buffer", bytes.Repeat([]byte("0123456789abcdef"), 4096)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mr, rcp := newRigrcpRunner(tc.content)

			content, err := remotefs.NewWindowsFS(mr).ReadFile(`C:\app\file.txt`)
			require.NoError(t, err)
			require.NotNil(t, content)
			require.Equal(t, tc.content, content)
			// The decisive assertion: the fake serves partial reads correctly
			// too, so matching content alone would pass against io.ReadAll,
			// which reads through a round trip per small buffer.
			require.Equal(t, []string{`o Open Read C:\app\file.txt`, "r -1 0", "c", "q"}, rcp.received())
		})
	}
}

func TestWindowsWriteFile(t *testing.T) {
	mr, rcp := newRigrcpRunner([]byte("0123456789"))

	require.NoError(t, remotefs.NewWindowsFS(mr).WriteFile(`C:\app\file.txt`, []byte("ab"), 0o644))
	require.Equal(t, "ab", string(rcp.file()), "a shorter write must not leave the old tail behind")
	require.Equal(t, []string{`o Create Write C:\app\file.txt`, "w 2 0", "c", "q"}, rcp.received())
}

func TestWindowsOpenFileAppend(t *testing.T) {
	mr, rcp := newRigrcpRunner([]byte("abc"))

	f, err := remotefs.NewWindowsFS(mr).OpenFile(`C:\app\file.txt`, os.O_APPEND|os.O_RDWR, 0)
	require.NoError(t, err)
	buf := make([]byte, 1)
	_, err = f.Read(buf)
	require.NoError(t, err)
	require.Equal(t, "a", string(buf), "an append handle still reads from the beginning")
	_, err = f.Write([]byte("de"))
	require.NoError(t, err)

	_, err = f.Seek(0, io.SeekStart)
	require.NoError(t, err)
	_, err = f.Read(buf)
	require.NoError(t, err)
	require.Equal(t, "a", string(buf), "seeking still positions reads")

	_, err = f.Write([]byte("f"))
	require.NoError(t, err)
	pos, err := f.Seek(0, io.SeekCurrent)
	require.NoError(t, err)
	require.Equal(t, int64(6), pos, "an append leaves the position after what it wrote")
	require.NoError(t, f.Close())

	require.Equal(t, "abcdef", string(rcp.file()), "every write goes to the end, wherever Seek left the position")
	// FileMode.Append would refuse ReadWrite and create a missing file, so
	// the file is opened plainly and each write aimed at the end.
	require.Equal(t, []string{`o Open ReadWrite C:\app\file.txt`, "r 1 0", "w 2 3", "r 1 0", "w 1 5", "c", "q"}, rcp.received())
}

func TestWindowsOpenFileAccess(t *testing.T) {
	const name = `C:\app\file.txt`

	t.Run("read-only rejects writes of any size", func(t *testing.T) {
		mr, rcp := newRigrcpRunner([]byte("abc"))
		f, err := remotefs.NewWindowsFS(mr).OpenFile(name, os.O_RDONLY, 0)
		require.NoError(t, err)

		_, err = f.Write(nil)
		require.ErrorIs(t, err, fs.ErrClosed, "an empty write is refused like any other")
		_, err = f.Write([]byte("x"))
		require.ErrorIs(t, err, fs.ErrClosed)
		require.NoError(t, f.Close())

		require.Equal(t, "abc", string(rcp.file()))
		require.Equal(t, []string{`o Open Read C:\app\file.txt`, "c", "q"}, rcp.received())
	})

	for _, tc := range []struct {
		name  string
		flags int
		host  string
	}{
		{"read-only append", os.O_RDONLY | os.O_APPEND, "Open ReadWrite"},
		{"read-only truncate", os.O_RDONLY | os.O_TRUNC, "Truncate ReadWrite"},
	} {
		t.Run(tc.name+" still rejects writes", func(t *testing.T) {
			mr, rcp := newRigrcpRunner([]byte("abc"))
			f, err := remotefs.NewWindowsFS(mr).OpenFile(name, tc.flags, 0)
			require.NoError(t, err)

			// The host is given write access only so that it accepts the
			// mode; the caller asked for a read-only handle.
			_, err = f.Write([]byte("x"))
			require.ErrorIs(t, err, fs.ErrClosed)
			require.NoError(t, f.Close())

			require.Equal(t, []string{"o " + tc.host + ` C:\app\file.txt`, "c", "q"}, rcp.received())
		})
	}

	t.Run("CopyFrom checks access even with nothing to copy", func(t *testing.T) {
		mr, _ := newRigrcpRunner([]byte("abc"))
		f, err := remotefs.NewWindowsFS(mr).OpenFile(name, os.O_RDONLY, 0)
		require.NoError(t, err)

		_, err = f.CopyFrom(bytes.NewReader(nil))
		require.ErrorIs(t, err, fs.ErrClosed, "a read-only handle is not writable")
		require.NoError(t, f.Close())
		_, err = f.CopyFrom(bytes.NewReader(nil))
		require.ErrorIs(t, err, fs.ErrClosed, "a closed handle is not writable")
	})

	t.Run("write-only rejects reads", func(t *testing.T) {
		mr, rcp := newRigrcpRunner([]byte("abc"))
		f, err := remotefs.NewWindowsFS(mr).OpenFile(name, os.O_WRONLY, 0)
		require.NoError(t, err)

		_, err = f.Read(make([]byte, 1))
		require.ErrorIs(t, err, fs.ErrClosed)
		_, err = f.CopyTo(io.Discard)
		require.ErrorIs(t, err, fs.ErrClosed)
		require.NoError(t, f.Close())

		require.Equal(t, []string{`o Open Write C:\app\file.txt`, "c", "q"}, rcp.received())
	})
}

func TestWindowsOpenFileAppendRejected(t *testing.T) {
	mr, rcp := newRigrcpRunner([]byte("abc"))
	rcp.rejectWrite = "The process cannot access the file."
	f, err := remotefs.NewWindowsFS(mr).OpenFile(`C:\app\file.txt`, os.O_APPEND|os.O_RDWR, 0)
	require.NoError(t, err)

	_, err = f.Write([]byte("x"))
	require.ErrorContains(t, err, "cannot access")
	pos, err := f.Seek(0, io.SeekCurrent)
	require.NoError(t, err)
	require.Zero(t, pos, "a write that was refused must not move the position to the end")
	require.NoError(t, f.Close())
}

// writeCommandSize mirrors the most payload remotefs sends in one w command.
const writeCommandSize = 4 << 20

// smallReader hands out at most max bytes per Read, and hides any
// io.WriterTo of the reader it wraps.
type smallReader struct {
	r   io.Reader
	max int
}

func (s smallReader) Read(p []byte) (int, error) {
	return s.r.Read(p[:min(len(p), s.max)]) //nolint:wrapcheck // test double
}

// writeCommands returns just the w commands the helper received.
func writeCommands(rcp *fakeRigrcp) []string {
	var writes []string
	for _, c := range rcp.received() {
		if strings.HasPrefix(c, "w ") {
			writes = append(writes, c)
		}
	}
	return writes
}

func TestWindowsFileWriteSplitsLargePayload(t *testing.T) {
	mr, rcp := newRigrcpRunner(nil)
	f, err := remotefs.NewWindowsFS(mr).OpenFile(`C:\app\file.txt`, os.O_WRONLY, 0)
	require.NoError(t, err)

	payload := bytes.Repeat([]byte("0123456789abcdef"), (2*writeCommandSize+16)/16)
	n, err := f.Write(payload)
	require.NoError(t, err)
	require.Equal(t, len(payload), n)
	require.NoError(t, f.Close())

	require.Equal(t, payload, rcp.file())
	require.Equal(t, []string{
		fmt.Sprintf("w %d 0", writeCommandSize),
		fmt.Sprintf("w %d %d", writeCommandSize, writeCommandSize),
		fmt.Sprintf("w 16 %d", 2*writeCommandSize),
	}, writeCommands(rcp), "the helper must never be handed more than one command's worth at once")
}

func TestWindowsFileCopyFrom(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), writeCommandSize+1000)
	want := []string{fmt.Sprintf("w %d 0", writeCommandSize), fmt.Sprintf("w 1000 %d", writeCommandSize)}

	for _, tc := range []struct {
		name string
		src  func(t *testing.T) io.Reader
	}{
		// Upload's io.TeeReader has no WriteTo and returns what its source
		// does per Read: io.Copy would make each of those a command.
		{"small reads", func(*testing.T) io.Reader { return smallReader{r: bytes.NewReader(payload), max: 32 * 1024} }},
		{"io.WriterTo", func(*testing.T) io.Reader { return bytes.NewReader(payload) }},
		// Its WriteTo writes 32 KiB at a time to anything but a socket.
		{"local file", func(t *testing.T) io.Reader {
			t.Helper()
			local, err := os.CreateTemp(t.TempDir(), "copyfrom")
			require.NoError(t, err)
			t.Cleanup(func() { _ = local.Close() })
			_, err = local.Write(payload)
			require.NoError(t, err)
			_, err = local.Seek(0, io.SeekStart)
			require.NoError(t, err)
			return local
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mr, rcp := newRigrcpRunner(nil)
			f, err := remotefs.NewWindowsFS(mr).OpenFile(`C:\app\file.txt`, os.O_WRONLY, 0)
			require.NoError(t, err)

			n, err := f.CopyFrom(tc.src(t))
			require.NoError(t, err)
			require.Equal(t, int64(len(payload)), n)
			require.NoError(t, f.Close())

			require.Equal(t, payload, rcp.file())
			require.Equal(t, want, writeCommands(rcp))
		})
	}

	t.Run("a pipe producer writing small chunks", func(t *testing.T) {
		mr, rcp := newRigrcpRunner(nil)
		f, err := remotefs.NewWindowsFS(mr).OpenFile(`C:\app\file.txt`, os.O_WRONLY, 0)
		require.NoError(t, err)

		// Each pipe Write blocks until CopyFrom has taken the bytes, which
		// batching does straight away: it must not wait for the producer.
		pr, pw := io.Pipe()
		go func() {
			for range 3 {
				if _, err := pw.Write([]byte("chunk")); err != nil {
					return
				}
			}
			_ = pw.Close()
		}()
		done := make(chan error, 1)
		go func() {
			_, err := f.CopyFrom(pr)
			done <- err
		}()
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("CopyFrom did not finish reading from a pipe")
		}
		require.NoError(t, f.Close())
		require.Equal(t, "chunkchunkchunk", string(rcp.file()))
	})

	t.Run("a read error is the source's", func(t *testing.T) {
		mr, rcp := newRigrcpRunner(nil)
		f, err := remotefs.NewWindowsFS(mr).OpenFile(`C:\app\file.txt`, os.O_WRONLY, 0)
		require.NoError(t, err)

		broken := errors.New("source broke")
		src := smallReader{r: io.MultiReader(bytes.NewReader([]byte("abc")), iotest.ErrReader(broken)), max: 32 * 1024}
		n, err := f.CopyFrom(src)
		require.ErrorIs(t, err, broken)
		require.Equal(t, int64(3), n, "what was read before the error is still written")
		require.NoError(t, f.Close())
		require.Equal(t, "abc", string(rcp.file()))
	})
}

// failingWriter accepts nothing.
type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestWindowsFileCopyToFailingDestination(t *testing.T) {
	mr, _ := newRigrcpRunner(bytes.Repeat([]byte("x"), 64*1024))
	f, err := remotefs.NewWindowsFS(mr).OpenFile(`C:\app\file.txt`, os.O_RDONLY, 0)
	require.NoError(t, err)

	sinkFull := errors.New("sink is full")
	_, err = f.CopyTo(failingWriter{err: sinkFull})
	require.ErrorIs(t, err, sinkFull)
	// The rest of the file is still in the stream, so the next reply would
	// be file content: the handle must refuse to be used instead.
	_, err = f.Read(make([]byte, 1))
	require.ErrorIs(t, err, fs.ErrClosed)
}

func TestWindowsFileWriteFailure(t *testing.T) {
	mr, rcp := newRigrcpRunner([]byte("abc"))
	rcp.writeErr = "There is not enough space on the disk."
	f, err := remotefs.NewWindowsFS(mr).OpenFile(`C:\app\file.txt`, os.O_RDWR, 0)
	require.NoError(t, err)
	_, err = f.Seek(0, io.SeekEnd)
	require.NoError(t, err)

	n, err := f.Write([]byte("xyz"))
	require.ErrorContains(t, err, "not enough space", "the write that failed must be the one to report it")
	require.Zero(t, n)
	// Part of the payload may have reached the file, so the handle's idea of
	// its position and end can no longer be trusted: it must not be reused.
	_, err = f.Seek(0, io.SeekEnd)
	require.ErrorIs(t, err, fs.ErrClosed)
	_, err = f.Write([]byte("x"))
	require.ErrorIs(t, err, fs.ErrClosed)

	require.Equal(t, "abc", string(rcp.file()))
	require.Equal(t, []string{`o Open ReadWrite C:\app\file.txt`, "w 3 3"}, rcp.received())
}

func TestWindowsFileSeek(t *testing.T) {
	const name = `C:\app\file.txt`

	t.Run("reads and writes land at the sought offset", func(t *testing.T) {
		mr, rcp := newRigrcpRunner([]byte("0123456789"))
		f, err := remotefs.NewWindowsFS(mr).OpenFile(name, os.O_RDWR, 0)
		require.NoError(t, err)

		pos, err := f.Seek(-4, io.SeekEnd)
		require.NoError(t, err)
		require.Equal(t, int64(6), pos)
		buf := make([]byte, 2)
		n, err := f.Read(buf)
		require.NoError(t, err)
		require.Equal(t, "67", string(buf[:n]))

		pos, err = f.Seek(-5, io.SeekCurrent)
		require.NoError(t, err)
		require.Equal(t, int64(3), pos)
		_, err = f.Write([]byte("ab"))
		require.NoError(t, err)

		pos, err = f.Seek(0, io.SeekEnd)
		require.NoError(t, err)
		require.Equal(t, int64(10), pos)
		_, err = f.Write([]byte("XY"))
		require.NoError(t, err)

		pos, err = f.Seek(0, io.SeekEnd)
		require.NoError(t, err)
		require.Equal(t, int64(12), pos, "a write past the end extends the size Seek measures from")

		_, err = f.Seek(1, io.SeekStart)
		require.NoError(t, err)
		var out bytes.Buffer
		_, err = f.CopyTo(&out)
		require.NoError(t, err)
		require.Equal(t, "12ab56789XY", out.String())
		require.NoError(t, f.Close())

		require.Equal(t, "012ab56789XY", string(rcp.file()))
		// Seek itself must not reach the helper: each operation carries its
		// own offset instead.
		require.Equal(t, []string{
			`o Open ReadWrite C:\app\file.txt`,
			"r 2 6",
			"w 2 3",
			"w 2 10",
			"r -1 1",
			"c",
			"q",
		}, rcp.received())
	})

	t.Run("an empty write past the end leaves the end alone", func(t *testing.T) {
		mr, rcp := newRigrcpRunner([]byte("abc"))
		f, err := remotefs.NewWindowsFS(mr).OpenFile(name, os.O_RDWR, 0)
		require.NoError(t, err)

		_, err = f.Seek(10, io.SeekStart)
		require.NoError(t, err)
		n, err := f.Write(nil)
		require.NoError(t, err)
		require.Zero(t, n)
		end, err := f.Seek(0, io.SeekEnd)
		require.NoError(t, err)
		require.Equal(t, int64(3), end, "writing nothing must not move the end of the file")
		require.NoError(t, f.Close())

		require.Equal(t, "abc", string(rcp.file()))
		require.Equal(t, []string{`o Open ReadWrite C:\app\file.txt`, "c", "q"}, rcp.received(), "an empty write needs no round trip")
	})

	t.Run("invalid", func(t *testing.T) {
		mr, rcp := newRigrcpRunner([]byte("abc"))
		f, err := remotefs.NewWindowsFS(mr).OpenFile(name, os.O_RDONLY, 0)
		require.NoError(t, err)

		_, err = f.Seek(-1, io.SeekStart)
		require.ErrorIs(t, err, fs.ErrInvalid)
		_, err = f.Seek(0, 42)
		require.ErrorIs(t, err, fs.ErrInvalid)
		pos, err := f.Seek(0, io.SeekCurrent)
		require.NoError(t, err)
		require.Equal(t, int64(0), pos, "a rejected seek must leave the position alone")

		require.NoError(t, f.Close())
		_, err = f.Seek(0, io.SeekStart)
		require.ErrorIs(t, err, fs.ErrClosed)
		require.Equal(t, []string{`o Open Read C:\app\file.txt`, "c", "q"}, rcp.received())
	})
}
