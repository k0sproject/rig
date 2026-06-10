package remotefs_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"io/fs"
	"os"
	"testing"
	"time"

	"github.com/k0sproject/rig/v2/remotefs"
	"github.com/k0sproject/rig/v2/rigtest"
	"github.com/stretchr/testify/assert"
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

func TestPosixHTTPStatus(t *testing.T) {
	t.Run("200", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandOutput(rigtest.Equal("command -v curl"), "/usr/bin/curl")
		mr.AddCommandOutput(rigtest.Equal("command -v base64"), "/usr/bin/base64")
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
		mr.AddCommandOutput(rigtest.Equal("command -v base64"), "/usr/bin/base64")
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
