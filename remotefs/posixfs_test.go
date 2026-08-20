package remotefs_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	osexec "os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/protocol/localhost"
	"github.com/k0sproject/rig/v2/remotefs"
	"github.com/k0sproject/rig/v2/rigtest"
	"github.com/k0sproject/rig/v2/sh"
	"github.com/k0sproject/rig/v2/sudo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
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

// touchCommands returns the touch commands the runner received for name, in the
// order they were issued.
func touchCommands(mr *rigtest.MockRunner, name string) []string {
	var commands []string
	for _, command := range mr.Commands() {
		if strings.Contains(command, "TZ=UTC touch") && strings.Contains(command, name) {
			commands = append(commands, command)
		}
	}
	return commands
}

func TestPosixChtimes(t *testing.T) {
	atime := time.Date(2024, 1, 15, 10, 30, 0, 123456789, time.UTC)
	mtime := time.Date(2025, 2, 16, 11, 31, 1, 987654321, time.UTC)

	// The access and modification times must be set in separate invocations: -d
	// is a single valued option, so a repeated one is either an error (uutils
	// coreutils) or silently ignored in favor of the last one (GNU touch), which
	// would leave the access time set to the modification timestamp.
	t.Run("nanosecond precision", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandOutput(rigtest.Equal("echo ${TMPDIR:-/tmp}"), "/tmp")
		// initStat probes for GNU stat, initTouch for a non-busybox touch.
		mr.AddCommandSuccess(rigtest.Equal("stat -c %n /"))
		mr.AddCommandOutput(rigtest.Equal("touch --help 2>&1"), "touch (GNU coreutils) 9.7")
		// initTouch probes nanosecond support on a temp file it creates and then
		// removes. Creating it stats the parent directory first; 0x41ed is 0o40755
		// (directory, rwxr-xr-x). The matchers are consulted in the order they were
		// added, so this one has to precede the one for the temp file itself.
		mr.AddCommandOutput(rigtest.Contains("-- /tmp 2>"), "0x41ed 4096 0.000000000 ///tmp//\n")
		// The temp file only exists once the create command has run; 0x8180 is
		// 0o100600 (regular file, rw-------).
		var created bool
		mr.AddCommand(rigtest.Contains("install -m 0600 /dev/null"), func(_ *rigtest.A) error {
			created = true
			return nil
		})
		mr.AddCommand(rigtest.Contains("stat -c '%#f"), func(a *rigtest.A) error {
			if !created {
				return nil
			}
			_, err := fmt.Fprintf(a.Stdout, "0x8180 0 0.000000000 //%s//\n", "/tmp/probe")
			return err
		})
		mr.AddCommandSuccess(rigtest.Contains("TZ=UTC touch"))
		mr.AddCommandSuccess(rigtest.HasPrefix("rm -f"))
		f := remotefs.NewPosixFS(mr)
		require.NoError(t, f.Chtimes("/tmp/file", atime.UnixNano(), mtime.UnixNano()))
		require.Equal(t, []string{
			`[ -e /tmp/file ] && env -i PATH="$PATH" LC_ALL=C TZ=UTC touch -a -d 2024-01-15T10:30:00.123456789 -- /tmp/file`,
			`[ -e /tmp/file ] && env -i PATH="$PATH" LC_ALL=C TZ=UTC touch -m -d 2025-02-16T11:31:01.987654321 -- /tmp/file`,
		}, touchCommands(mr, "/tmp/file"))
	})

	t.Run("second precision", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		// A busybox touch takes the second precision path, which needs no probing.
		mr.AddCommandOutput(rigtest.Equal("touch --help 2>&1"), "BusyBox v1.35")
		mr.AddCommandSuccess(rigtest.Contains("TZ=UTC touch"))
		f := remotefs.NewPosixFS(mr)
		require.NoError(t, f.Chtimes("/tmp/file", atime.UnixNano(), mtime.UnixNano()))
		require.Equal(t, []string{
			`[ -e /tmp/file ] && env -i PATH="$PATH" LC_ALL=C TZ=UTC touch -a -d @1705314600 -- /tmp/file`,
			`[ -e /tmp/file ] && env -i PATH="$PATH" LC_ALL=C TZ=UTC touch -m -d @1739705461 -- /tmp/file`,
		}, touchCommands(mr, "/tmp/file"))
	})
}

func TestPosixWriteFile(t *testing.T) {
	// The command must not name /dev/stdin as an input file: the uutils
	// reimplementation of coreutils refuses non-regular files there.
	t.Run("command", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		var got []byte
		mr.AddCommand(rigtest.Contains("cat >"), func(a *rigtest.A) error {
			var err error
			got, err = io.ReadAll(a.Stdin)
			return err
		})
		f := remotefs.NewPosixFS(mr)
		require.NoError(t, f.WriteFile("/etc/k0s/k0s.yaml", []byte("hello"), 0o600))
		require.Equal(t,
			"mkdir -p -- /etc/k0s && umask 0177 && cat >/etc/k0s/k0s.yaml && chmod -- 0600 /etc/k0s/k0s.yaml",
			mr.LastCommand(),
		)
		require.Equal(t, "hello", string(got))
		require.NoError(t, mr.NotReceived(rigtest.Contains("/dev/stdin")))
	})

	t.Run("umask complements the requested permissions", func(t *testing.T) {
		for _, tc := range []struct {
			name  string
			perm  fs.FileMode
			umask string
			chmod string
		}{
			{name: "executable", perm: 0o755, umask: "umask 022", chmod: "chmod -- 0755 /tmp/f"},
			{name: "world readable", perm: 0o644, umask: "umask 0133", chmod: "chmod -- 0644 /tmp/f"},
			{name: "no permissions", perm: 0, umask: "umask 0777", chmod: "chmod -- 0 /tmp/f"},
			// The special bits are translated to their POSIX octal digit. A
			// umask only covers the nine permission bits, so they are applied
			// by the chmod alone.
			{name: "setuid", perm: fs.ModeSetuid | 0o600, umask: "umask 0177", chmod: "chmod -- 04600 /tmp/f"},
			{name: "setgid", perm: fs.ModeSetgid | 0o755, umask: "umask 022", chmod: "chmod -- 02755 /tmp/f"},
			{name: "sticky", perm: fs.ModeSticky | 0o770, umask: "umask 07 &&", chmod: "chmod -- 01770 /tmp/f"},
			// File type bits have no chmod representation and are ignored.
			{name: "type bits ignored", perm: fs.ModeDir | 0o755, umask: "umask 022", chmod: "chmod -- 0755 /tmp/f"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				mr := rigtest.NewMockRunner()
				mr.AddCommandSuccess(rigtest.Contains("cat >"))
				f := remotefs.NewPosixFS(mr)
				require.NoError(t, f.WriteFile("/tmp/f", []byte("x"), tc.perm))
				require.Contains(t, mr.LastCommand(), tc.umask)
				require.Contains(t, mr.LastCommand(), tc.chmod)
			})
		}
	})

	t.Run("quotes paths with spaces", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("cat >"))
		f := remotefs.NewPosixFS(mr)
		require.NoError(t, f.WriteFile("/tmp/my dir/file", []byte("x"), 0o644))
		require.Equal(t,
			`mkdir -p -- '/tmp/my dir' && umask 0133 && cat >'/tmp/my dir/file' && chmod -- 0644 '/tmp/my dir/file'`,
			mr.LastCommand(),
		)
	})

	t.Run("command fails", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandFailure(rigtest.Contains("cat >"), errors.New("permission denied"))
		f := remotefs.NewPosixFS(mr)
		err := f.WriteFile("/etc/k0s/k0s.yaml", []byte("hello"), 0o600)
		require.ErrorContains(t, err, "write file /etc/k0s/k0s.yaml")
	})
}

// modeCases covers the fs.FileMode → POSIX octal translation shared by every
// command that takes a mode. The special bits live in the high bits of an
// fs.FileMode, so formatting one directly would produce a nonsensical number.
var modeCases = []struct {
	name string
	mode fs.FileMode
	want string
}{
	{name: "permission bits", mode: 0o750, want: "0750"},
	{name: "setuid", mode: fs.ModeSetuid | 0o755, want: "04755"},
	{name: "setgid", mode: fs.ModeSetgid | 0o775, want: "02775"},
	{name: "sticky", mode: fs.ModeSticky | 0o777, want: "01777"},
	// File type bits have no chmod representation and are ignored.
	{name: "type bits ignored", mode: fs.ModeDir | 0o700, want: "0700"},
}

func TestPosixMkdir(t *testing.T) {
	for _, tc := range modeCases {
		t.Run(tc.name, func(t *testing.T) {
			mr := rigtest.NewMockRunner()
			mr.AddCommandSuccess(rigtest.Contains("mkdir"))
			f := remotefs.NewPosixFS(mr)
			require.NoError(t, f.Mkdir("/tmp/dir", tc.mode))
			require.Equal(t, "mkdir -m "+tc.want+" /tmp/dir", mr.LastCommand())
		})
	}
}

func TestPosixMkdirAll(t *testing.T) {
	// MkdirAll stats the target first; an unmatched stat yields no output, which
	// reads as "does not exist" and lets it proceed to create the directories.
	//
	// The permission bits come from a umask so that they also apply to the
	// intermediate directories; only the special bits need a chmod, which reaches
	// the last directory of the path alone.
	for _, tc := range []struct {
		name string
		mode fs.FileMode
		want string
	}{
		{name: "permission bits", mode: 0o750, want: "umask 027 && mkdir -p -- /tmp/a/b"},
		{name: "setuid", mode: fs.ModeSetuid | 0o755, want: "umask 022 && mkdir -p -- /tmp/a/b && chmod -- 04755 /tmp/a/b"},
		{name: "setgid", mode: fs.ModeSetgid | 0o775, want: "umask 02 && mkdir -p -- /tmp/a/b && chmod -- 02775 /tmp/a/b"},
		{name: "sticky", mode: fs.ModeSticky | 0o777, want: "umask 0 && mkdir -p -- /tmp/a/b && chmod -- 01777 /tmp/a/b"},
		{name: "type bits ignored", mode: fs.ModeDir | 0o700, want: "umask 077 && mkdir -p -- /tmp/a/b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mr := rigtest.NewMockRunner()
			mr.AddCommandSuccess(rigtest.Contains("mkdir -p"))
			f := remotefs.NewPosixFS(mr)
			require.NoError(t, f.MkdirAll("/tmp/a/b", tc.mode))
			require.Equal(t, tc.want, mr.LastCommand())
		})
	}
}

func TestPosixOpenFileCreate(t *testing.T) {
	for _, tc := range modeCases {
		t.Run(tc.name, func(t *testing.T) {
			mr := rigtest.NewMockRunner()
			// initStat probes for GNU stat.
			mr.AddCommandSuccess(rigtest.Equal("stat -c %n /"))
			var created bool
			// 0x81a4 = 0o100644 (regular file, rw-r--r--). The file only exists
			// once install has run.
			mr.AddCommand(rigtest.Contains("-- /tmp/new"), func(a *rigtest.A) error {
				if !created {
					return nil
				}
				_, err := a.Stdout.Write([]byte("0x81a4 0 1234567890.000000000 ///tmp/new//\n"))
				return err
			})
			mr.AddCommand(rigtest.Contains("install"), func(_ *rigtest.A) error {
				created = true
				return nil
			})
			// 0x41ed = 0o40755 (directory, rwxr-xr-x) for the parent.
			mr.AddCommandOutput(rigtest.Contains("stat -c"), "0x41ed 0 1234567890.000000000 ///tmp//")

			f := remotefs.NewPosixFS(mr)
			file, err := f.OpenFile("/tmp/new", os.O_CREATE|os.O_WRONLY, tc.mode)
			require.NoError(t, err)
			require.NoError(t, file.Close())
			require.NoError(t, mr.Received(rigtest.Equal("install -m "+tc.want+" /dev/null /tmp/new")))
		})
	}
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

func TestPosixChmod(t *testing.T) {
	t.Run("modes", func(t *testing.T) {
		for _, tc := range modeCases {
			t.Run(tc.name, func(t *testing.T) {
				mr := rigtest.NewMockRunner()
				mr.AddCommandSuccess(rigtest.Contains("chmod"))
				f := remotefs.NewPosixFS(mr)
				require.NoError(t, f.Chmod("/tmp/file", tc.mode))
				require.Equal(t, "chmod "+tc.want+" /tmp/file", mr.LastCommand())
			})
		}
	})

	t.Run("not exist", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandFailure(rigtest.Contains("chmod"), errors.New("No such file or directory"))
		f := remotefs.NewPosixFS(mr)
		require.ErrorIs(t, f.Chmod("/tmp/file", 0o644), fs.ErrNotExist)
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

func TestPosixInitStat(t *testing.T) {
	// initStat selects between GNU and BSD stat by inspecting stat's capabilities.
	// GNU mode tests and uses "stat -c", BSD mode tests "stat -s" and uses "stat -f".
	type matchers = []rigtest.CommandMatcher
	cases := []struct {
		name     string
		expected matchers
	}{
		{"GNU", matchers{rigtest.Equal("stat -c %n /"), rigtest.Contains("LC_ALL=C stat -c")}},
		{"BSD", matchers{rigtest.Equal("stat -s /"), rigtest.Contains("LC_ALL=C stat -f")}},
		{"unknown", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mr := rigtest.NewMockRunner()
			mr.ErrDefault = errors.New("unexpected command")
			for _, expected := range tc.expected {
				mr.AddCommandSuccess(expected)
			}

			f := remotefs.NewPosixFS(mr)
			_, err := f.Stat("/tmp/file")

			if tc.expected == nil {
				assert.ErrorContains(t, err, "unsupported stat implementation")
			} else {
				assert.ErrorIs(t, err, os.ErrNotExist)
				for i, expected := range tc.expected {
					if err := mr.Received(expected); err != nil {
						assert.Failf(t, "Expected command not received", "Command %d", i)
					}
				}
			}
		})
	}
}

func TestPosixStatModTime(t *testing.T) {
	// The modification time is the third field of the stat output: a GNU style stat
	// prints it as the date, time and UTC offset of %y, a BSD one as the epoch seconds
	// of %Fm. Both forms are parsed, and the file name follows the timestamp in both.
	const (
		modTimeNano = int64(1699977296220228000) // 2023-11-14 15:54:56.220228 UTC
		wholeSecond = int64(1699977296000000000)
	)
	cases := []struct {
		name        string
		output      string
		wantModTime int64
		wantName    string
		wantErr     string
	}{
		{"date and time", "0x81a4 12 2023-11-14 15:54:56.220228000 +0000 ///tmp/file//", modTimeNano, "file", ""},
		{"date and time in another zone", "0x81a4 12 2023-11-14 17:54:56.220228000 +0200 ///tmp/file//", modTimeNano, "file", ""},
		{"date and time without a fraction", "0x81a4 12 2023-11-14 15:54:56 +0000 ///tmp/file//", wholeSecond, "file", ""},
		{"epoch with a fraction", "0x81a4 12 1699977296.220228000 ///tmp/file//", modTimeNano, "file", ""},
		{"epoch without a fraction", "0x81a4 12 1699977296 ///tmp/file//", wholeSecond, "file", ""},
		{"name with spaces", "0x81a4 12 2023-11-14 15:54:56.220228000 +0000 ///tmp/two words//", modTimeNano, "two words", ""},
		{"timestamp without an offset", "0x81a4 12 2023-11-14 15:54:56.220228000 ///tmp/file//", 0, "", "missing its time or offset"},
		{"date that does not exist", "0x81a4 12 2023-13-45 15:54:56.220228000 +0000 ///tmp/file//", 0, "", "parse timestamp"},
		{"epoch that is not a number", "0x81a4 12 not-a-time ///tmp/file//", 0, "", "parse epoch seconds"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mr := rigtest.NewMockRunner()
			// initStat probes for GNU stat.
			mr.AddCommandSuccess(rigtest.Equal("stat -c %n /"))
			mr.AddCommandOutput(rigtest.Contains("LC_ALL=C stat -c"), tc.output)

			info, err := remotefs.NewPosixFS(mr).Stat("/tmp/file")
			if tc.wantErr != "" {
				assert.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantModTime, info.ModTime().UnixNano())
			assert.Equal(t, tc.wantName, info.Name())
			assert.Equal(t, int64(12), info.Size())
		})
	}
}

func TestPosixGetenv(t *testing.T) {
	t.Run("valid key executes command", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandOutput(rigtest.Contains("HOME"), "/home/user")
		f := remotefs.NewPosixFS(mr)
		require.Equal(t, "/home/user", f.Getenv("HOME"))
		require.NoError(t, mr.Received(rigtest.Contains("HOME")))
	})
	t.Run("injection attempt returns empty without executing", func(t *testing.T) {
		for _, key := range []string{`FOO}"`, `}; rm -rf /`, `A=B`, `FOO bar`, ``} {
			mr := rigtest.NewMockRunner()
			f := remotefs.NewPosixFS(mr)
			require.Empty(t, f.Getenv(key), "key: %q", key)
			require.Equal(t, 0, mr.Len(), "no command should be run for key: %q", key)
		}
	})
}

func TestPosixCreateTemp(t *testing.T) {
	t.Run("with prefix", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandOutput(rigtest.Equal("echo ${TMPDIR:-/tmp}"), "/tmp")
		mr.AddCommandOutput(rigtest.Contains("mktemp"), "/tmp/rig-abc123")
		f := remotefs.NewPosixFS(mr)
		path, err := f.CreateTemp("", "rig-")
		require.NoError(t, err)
		require.Equal(t, "/tmp/rig-abc123", path)
		require.NoError(t, mr.Received(rigtest.Contains("mktemp -- /tmp/rig-XXXXXX")))
	})
	t.Run("failure", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandFailure(rigtest.Contains("mktemp"), errors.New("permission denied"))
		f := remotefs.NewPosixFS(mr)
		_, err := f.CreateTemp("/srv", "rig-")
		require.Error(t, err)
	})
}

func TestPosixFSFollow(t *testing.T) {
	const path = "/var/log/app.log"

	t.Run("output flows to writer", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommand(rigtest.Contains("tail"), func(a *rigtest.A) error {
			_, _ = a.Stdout.Write([]byte("new line\n"))
			return nil
		})
		fs := remotefs.NewPosixFS(mr)
		var buf bytes.Buffer
		require.NoError(t, fs.Follow(context.Background(), path, &buf))
		require.Equal(t, "new line\n", buf.String())
	})

	t.Run("starts from EOF", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("tail"))
		fs := remotefs.NewPosixFS(mr)
		_ = fs.Follow(context.Background(), path, io.Discard)
		require.NoError(t, mr.Received(rigtest.Contains("-n 0")))
	})

	t.Run("context cancellation returns nil", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		mr.AddCommand(rigtest.Contains("tail"), func(a *rigtest.A) error {
			return a.Ctx.Err()
		})
		fs := remotefs.NewPosixFS(mr)
		require.NoError(t, fs.Follow(ctx, path, io.Discard), "context cancellation should not return an error")
	})

	t.Run("command error propagates", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandFailure(rigtest.Contains("tail"), errors.New("permission denied"))
		fs := remotefs.NewPosixFS(mr)
		require.Error(t, fs.Follow(context.Background(), path, io.Discard))
	})
}

func TestPosixNativePath(t *testing.T) {
	mr := rigtest.NewMockRunner()
	pfs := remotefs.NewPosixFS(mr)

	cases := []struct {
		input string
		want  string
	}{
		{"/usr/local/bin", "/usr/local/bin"},
		{"relative/path", "relative/path"},
		{"no-slashes", "no-slashes"},
		{"", ""},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, pfs.NativePath(tc.input))
	}
}

func TestPosixShellQuote(t *testing.T) {
	mr := rigtest.NewMockRunner()
	pfs := remotefs.NewPosixFS(mr)

	cases := []struct {
		input string
		want  string
	}{
		{"", "''"},
		{"hello", "hello"},
		{"hello world", "'hello world'"},
		{"$var", "'$var'"},
		{"$(cmd)", "'$(cmd)'"},
		{"it's", "'it'\"'\"'s'"},
		{`back\slash`, `'back\slash'`},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, pfs.ShellQuote(tc.input), "input: %q", tc.input)
	}
}

func TestPosixNotExistByPathLength(t *testing.T) {
	// Whether a missing file is recognised as such must not depend on how long
	// its path is. The operations below have no structured way of reporting
	// absence, so they read the command's diagnostic -- and coreutils put the
	// path first and the reason last, so a long path is exactly what pushes the
	// reason out of a shortened message.
	ops := []struct {
		name string
		call func(fsys *remotefs.PosixFS, path string) error
	}{
		{"Sha256", func(fsys *remotefs.PosixFS, path string) error {
			_, err := fsys.Sha256(path)
			return err
		}},
		{"Chmod", func(fsys *remotefs.PosixFS, path string) error {
			return fsys.Chmod(path, 0o644)
		}},
		{"Chown", func(fsys *remotefs.PosixFS, path string) error {
			return fsys.Chown(path, "root:root")
		}},
		{"ChownInt", func(fsys *remotefs.PosixFS, path string) error {
			return fsys.ChownInt(path, 0, 0)
		}},
		{"ChownTree", func(fsys *remotefs.PosixFS, path string) error {
			return fsys.ChownTree(path, "root:root")
		}},
		{"ChownTreeInt", func(fsys *remotefs.PosixFS, path string) error {
			return fsys.ChownTreeInt(path, 0, 0)
		}},
	}

	for _, depth := range []int{0, 12, 24} {
		dir := "/tmp/" + strings.Repeat("deployments/", depth)
		missing := dir + "missing.conf"
		for _, op := range ops {
			t.Run(fmt.Sprintf("%s/pathlen=%d", op.name, len(missing)), func(t *testing.T) {
				mr := rigtest.NewMockRunner()
				mr.AddCommand(rigtest.Contains(missing), func(a *rigtest.A) error {
					// The coreutils diagnostic, reason last.
					fmt.Fprintf(a.Stderr, "cannot access '%s': No such file or directory\n", missing)
					return errors.New("exit status 1")
				})

				err := op.call(remotefs.NewPosixFS(mr), missing)
				require.Error(t, err)
				require.ErrorIs(t, err, fs.ErrNotExist,
					"a missing file must be recognised at any path length")
			})
		}
	}
}

func TestPosixNotExistThroughSudo(t *testing.T) {
	// The production path: a sudo-decorated runner chains a second Executor in
	// front of the first, so the command's stderr must still reach the error
	// value the classification reads.
	longPath := "/tmp/" + strings.Repeat("deployments/", 24) + "missing.conf"

	mr := rigtest.NewMockRunner()
	mr.AddCommand(rigtest.Contains(longPath), func(a *rigtest.A) error {
		fmt.Fprintf(a.Stderr, "cannot access '%s': No such file or directory\n", longPath)
		return errors.New("exit status 1")
	})
	sudoRunner := cmd.NewExecutor(mr, sudo.Sudo)

	// A control run first: at this path length the shortened message cannot carry
	// the reason, so classifying correctly is only possible from the full stderr.
	require.NotContains(t, sudoRunner.Exec(sh.Command("chmod", "0644", longPath)).Error(),
		"No such file or directory", "the fixture must not be classifiable from the message")

	err := remotefs.NewPosixFS(sudoRunner).Chmod(longPath, 0o644)
	require.ErrorIs(t, err, fs.ErrNotExist, "the full stderr must survive a chained runner")
	require.NoError(t, mr.Received(rigtest.HasPrefix("sudo")))
}

// sshExitError stands in for the exit error of the native SSH protocol, whose
// real type cannot be constructed with a chosen status. sshExitStatusPinned
// below asserts that the real type has the shape modelled here.
type sshExitError struct {
	status int
}

func (e sshExitError) Error() string { return fmt.Sprintf("Process exited with status %d", e.status) }

func (e sshExitError) ExitStatus() int { return e.status }

// The classification in multiStat reads these two interfaces off the errors the
// protocols produce. Pinning them here turns an upstream type change into a
// build failure rather than a silent regression.
var (
	_ interface{ ExitStatus() int } = (*ssh.ExitError)(nil)
	_ interface{ ExitStatus() int } = sshExitError{}
	_ interface{ ExitCode() int }   = (*osexec.ExitError)(nil)
)

// exitCommand returns a command line that makes the platform's shell exit with
// the given code. localhost and openssh run their processes through a local
// shell, so the fixtures below use the same one the tests run on.
func exitCommand(code int) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd.exe", []string{"/c", fmt.Sprintf("exit %d", code)}
	}
	return "sh", []string{"-c", fmt.Sprintf("exit %d", code)}
}

// localExitError runs a local process that exits with the given code and
// returns the resulting *exec.ExitError. A zero value of that type is
// unsuitable as a fixture: it reports ExitCode() == -1, which models a process
// terminated without a code rather than a normal non-zero exit.
func localExitError(t *testing.T, code int) error {
	t.Helper()
	name, args := exitCommand(code)
	err := osexec.Command(name, args...).Run()
	require.Error(t, err)
	var exitErr *osexec.ExitError
	require.ErrorAs(t, err, &exitErr)
	require.Equal(t, code, exitErr.ExitCode())
	return err
}

// signalKilledError runs a local process that kills itself with SIGKILL and
// returns the resulting *exec.ExitError, whose ExitCode() is -1.
//
// Windows has no signals, and TerminateProcess always supplies a code, so no
// local process there can report a negative one. The case is skipped rather
// than approximated: the classification it pins is about the absence of a code,
// which Windows cannot produce.
func signalKilledError(t *testing.T) error {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("a local process cannot exit without a code on Windows")
	}
	err := osexec.Command("sh", "-c", "kill -9 $$").Run()
	require.Error(t, err)
	var exitErr *osexec.ExitError
	require.ErrorAs(t, err, &exitErr)
	require.Negative(t, exitErr.ExitCode())
	return err
}

func TestPosixStatCommandFailure(t *testing.T) {
	// A stat that never ran says nothing about the file, so only a stat that ran
	// on the host and exited non-zero may be reported as fs.ErrNotExist. Every
	// other failure must surface as itself, with the original cause reachable.
	cases := []struct {
		name         string
		failWith     func(t *testing.T) error
		wantNotExist bool
	}{
		{
			name:         "remote stat ran and exited 1",
			failWith:     func(*testing.T) error { return sshExitError{status: 1} },
			wantNotExist: true,
		},
		{
			name:         "remote command exited 255",
			failWith:     func(*testing.T) error { return sshExitError{status: 255} },
			wantNotExist: true,
		},
		{
			name:         "remote command killed by a signal",
			failWith:     func(*testing.T) error { return sshExitError{status: -1} },
			wantNotExist: false,
		},
		{
			name: "session could not be started",
			failWith: func(*testing.T) error {
				return errors.New("start session: connection lost")
			},
			wantNotExist: false,
		},
		{
			name: "connection died mid-command",
			failWith: func(*testing.T) error {
				return fmt.Errorf("start session: %w", io.EOF)
			},
			wantNotExist: false,
		},
		{
			name:         "local process ran and exited 1",
			failWith:     func(t *testing.T) error { return localExitError(t, 1) },
			wantNotExist: true,
		},
		{
			name:         "openssh client could not connect",
			failWith:     func(t *testing.T) error { return localExitError(t, 255) },
			wantNotExist: false,
		},
		{
			name:         "local process killed by a signal",
			failWith:     signalKilledError,
			wantNotExist: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			failWith := tc.failWith(t)

			mr := rigtest.NewMockRunner()
			mr.AddCommandSuccess(rigtest.Equal("stat -c %n /"))
			mr.AddCommandFailure(rigtest.Contains("LC_ALL=C stat -c"), failWith)

			_, err := remotefs.NewPosixFS(mr).Stat("/etc/app.conf")
			require.Error(t, err)

			if tc.wantNotExist {
				require.ErrorIs(t, err, fs.ErrNotExist)
				return
			}
			require.NotErrorIs(t, err, fs.ErrNotExist,
				"a failure that says nothing about the file must not be reported as ErrNotExist")
			require.ErrorIs(t, err, failWith, "the original cause must stay reachable")
		})
	}
}

func TestPosixStatLocalhost(t *testing.T) {
	// The compatibility-critical direction, through the real localhost protocol
	// with no mocks: an existing file stats cleanly and a missing one is
	// ErrNotExist.
	if runtime.GOOS == "windows" {
		t.Skip("PosixFS is never paired with a Windows host")
	}
	dir := t.TempDir()
	existing := filepath.Join(dir, "existing.conf")
	require.NoError(t, os.WriteFile(existing, []byte("keep me\n"), 0o600))

	conn, err := localhost.NewConnection()
	require.NoError(t, err)
	fsys := remotefs.NewPosixFS(cmd.NewExecutor(conn))

	info, err := fsys.Stat(existing)
	require.NoError(t, err)
	require.Equal(t, int64(len("keep me\n")), info.Size())

	_, err = fsys.Stat(filepath.Join(dir, "missing.conf"))
	require.ErrorIs(t, err, fs.ErrNotExist)
}

func TestPosixReadDirConnectionLost(t *testing.T) {
	// A find that died mid-command reports a wrapped io.EOF. That is a lost
	// connection, not an empty or missing directory, and must not collapse into
	// fs.ErrNotExist.
	connLost := fmt.Errorf("start session: %w", io.EOF)

	mr := rigtest.NewMockRunner()
	mr.AddCommandFailure(rigtest.Contains("find"), connLost)

	_, err := remotefs.NewPosixFS(mr).ReadDir("/etc")
	require.Error(t, err)
	require.NotErrorIs(t, err, fs.ErrNotExist)
	require.ErrorIs(t, err, connLost)
}
