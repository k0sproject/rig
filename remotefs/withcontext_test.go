package remotefs_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/k0sproject/rig/v2/remotefs"
	"github.com/k0sproject/rig/v2/rigtest"
	"github.com/stretchr/testify/require"
)

type ctxKey struct{}

// recordContexts makes every command on runner succeed and records the value
// of ctxKey in the context each one ran under.
func recordContexts(runner *rigtest.MockRunner) *[]any {
	var seen []any
	runner.AddCommand(rigtest.Match("."), func(a *rigtest.A) error {
		seen = append(seen, a.Ctx.Value(ctxKey{}))
		return a.Ctx.Err() //nolint:wrapcheck // returned as is to the code under test
	})
	return &seen
}

func TestWithContext(t *testing.T) {
	for _, tc := range []struct {
		name    string
		windows bool
		newFS   func(*rigtest.MockRunner) remotefs.FS
		// op is an operation that runs commands without parsing their output.
		op func(remotefs.FS) error
	}{
		{
			"posix", false,
			func(r *rigtest.MockRunner) remotefs.FS { return remotefs.NewPosixFS(r) },
			func(f remotefs.FS) error { return f.Remove("file") },
		},
		{
			"windows", true,
			func(r *rigtest.MockRunner) remotefs.FS { return remotefs.NewWindowsFS(r) },
			func(f remotefs.FS) error { return f.Mkdir("dir", 0o755) },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("commands run under the context", func(t *testing.T) {
				runner := rigtest.NewMockRunner()
				runner.Windows = tc.windows
				seen := recordContexts(runner)
				fsys := tc.newFS(runner)

				ctx := context.WithValue(context.Background(), ctxKey{}, "bound")
				require.NoError(t, tc.op(remotefs.WithContext(ctx, fsys)))
				require.NotEmpty(t, *seen)
				for _, v := range *seen {
					require.Equal(t, "bound", v)
				}

				*seen = nil
				require.NoError(t, tc.op(fsys))
				require.NotEmpty(t, *seen)
				for _, v := range *seen {
					require.Nil(t, v, "the original filesystem is not bound")
				}
			})

			t.Run("a done context fails operations", func(t *testing.T) {
				runner := rigtest.NewMockRunner()
				runner.Windows = tc.windows
				recordContexts(runner)
				fsys := tc.newFS(runner)

				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				require.ErrorIs(t, tc.op(remotefs.WithContext(ctx, fsys)), context.Canceled)
				require.NoError(t, tc.op(fsys), "the original filesystem is unaffected")
			})

			t.Run("rebinding replaces the context", func(t *testing.T) {
				runner := rigtest.NewMockRunner()
				runner.Windows = tc.windows
				seen := recordContexts(runner)
				fsys := tc.newFS(runner)

				canceled, cancel := context.WithCancel(context.Background())
				cancel()
				ctx := context.WithValue(context.Background(), ctxKey{}, "second")
				rebound := remotefs.WithContext(ctx, remotefs.WithContext(canceled, fsys))
				require.NoError(t, tc.op(rebound))
				require.NotEmpty(t, *seen)
				for _, v := range *seen {
					require.Equal(t, "second", v)
				}
			})
		})
	}

	t.Run("a done context is reported by a first posix stat", func(t *testing.T) {
		runner := rigtest.NewMockRunner()
		recordContexts(runner)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := remotefs.WithContext(ctx, remotefs.NewPosixFS(runner)).Stat("file")
		require.ErrorIs(t, err, context.Canceled, "probing which stat the host has must not hide the cancellation")
	})

	t.Run("a cancelled touch probe is not cached", func(t *testing.T) {
		runner := rigtest.NewMockRunner()
		ctx, cancel := context.WithCancel(context.Background())
		runner.AddCommandOutput(rigtest.HasPrefix("touch --help"), "touch (GNU coreutils) 9.4\n")
		// Enough of a filesystem for the probe's temp file to be created.
		runner.AddCommandOutput(rigtest.HasPrefix("echo ${TMPDIR"), "/tmp\n")
		runner.AddCommandOutput(rigtest.HasPrefix("stat -c %n"), "/\n")
		runner.AddCommandOutput(rigtest.Contains("-- /tmp 2>"), "0x41ed 4096 2024-01-01 00:00:00.000000000 +0000 ///tmp//\n")
		statted := map[string]bool{}
		runner.AddCommand(rigtest.Contains("stat -c '%#f"), func(a *rigtest.A) error {
			name, _, _ := strings.Cut(a.Command[strings.Index(a.Command, "-- ")+3:], " 2>")
			// Missing on the check before it is created, there afterwards.
			if !statted[name] {
				statted[name] = true
				return nil
			}
			_, _ = fmt.Fprintf(a.Stdout, "0x81a4 0 2024-01-01 00:00:00.000000000 +0000 //%s//\n", name)
			return nil
		})
		var nsec int
		runner.AddCommand(rigtest.Contains("touch -"), func(a *rigtest.A) error {
			if strings.Contains(a.Command, "T00:00:00.000000000") {
				nsec++
				if nsec == 1 {
					cancel()
					return context.Canceled
				}
			}
			return nil
		})
		fsys := remotefs.NewPosixFS(runner)

		require.ErrorIs(t, remotefs.WithContext(ctx, fsys).Chtimes("file", 0, 0), context.Canceled)
		require.NoError(t, fsys.Chtimes("file", 0, 0))
		// One touch per timestamp: the cancelled probe's first, then the
		// repeated probe's two and the call's own two at nanosecond precision.
		require.Equal(t, 5, nsec, "the probe runs again and nanosecond precision is used")
	})

	t.Run("chtimes runs on the bound copy", func(t *testing.T) {
		runner := rigtest.NewMockRunner()
		var seen []any
		runner.AddCommandOutput(rigtest.HasPrefix("touch --help"), "BusyBox v1.36.1\n")
		runner.AddCommand(rigtest.Contains("touch -"), func(a *rigtest.A) error {
			seen = append(seen, a.Ctx.Value(ctxKey{}))
			return nil
		})
		fsys := remotefs.NewPosixFS(runner)
		// Probe touch on the original so that the copy inherits the result.
		require.NoError(t, fsys.Chtimes("file", 0, 0))

		seen = nil
		ctx := context.WithValue(context.Background(), ctxKey{}, "bound")
		bound, ok := remotefs.WithContext(ctx, fsys).(*remotefs.PosixFS)
		require.True(t, ok)
		require.NoError(t, bound.Chtimes("file", 0, 0))
		require.Equal(t, []any{"bound", "bound"}, seen)
	})

	// These operations start commands with an explicit context of their own
	// rather than through the runner's context-less methods. Windows commands
	// are encoded, so the mock tells them apart by order: the first is the
	// Stat that OpenFile starts with.
	for _, tc := range []struct {
		name string
		stat string
		op   func(remotefs.FS) error
	}{
		{
			"windows directory listing",
			`{"Name":"dir","FullName":"C:\\dir","Attributes":16,"Mode":"d-----","LastWriteTime":"/Date(0)/"}`,
			func(f remotefs.FS) error { _, err := f.ReadDir("dir"); return err },
		},
		{
			"windows file daemon",
			`{"Name":"file","FullName":"C:\\file","Attributes":32,"Mode":"-a----","LastWriteTime":"/Date(0)/"}`,
			func(f remotefs.FS) error { _, err := f.ReadFile("file"); return err },
		},
	} {
		t.Run(tc.name+" runs under the context", func(t *testing.T) {
			runner := rigtest.NewMockRunner()
			runner.Windows = true
			var seen []any
			runner.AddCommand(rigtest.Match("."), func(a *rigtest.A) error {
				seen = append(seen, a.Ctx.Value(ctxKey{}))
				if len(seen) == 1 {
					_, _ = a.Stdout.Write([]byte(tc.stat))
				}
				return nil
			})

			ctx := context.WithValue(context.Background(), ctxKey{}, "bound")
			// The mock does not speak the listing or rigrcp protocol, so the
			// operation itself may fail. What matters is which context the
			// command after the Stat ran under.
			_ = tc.op(remotefs.WithContext(ctx, remotefs.NewWindowsFS(runner)))
			require.GreaterOrEqual(t, len(seen), 2, "the command after the Stat must run")
			for _, v := range seen {
				require.Equal(t, "bound", v)
			}
		})
	}

	t.Run("posix DownloadURL runs under the context", func(t *testing.T) {
		runner := rigtest.NewMockRunner()
		var curl []any
		runner.AddCommandOutput(rigtest.HasPrefix("mktemp"), "/tmp/file.part\n")
		runner.AddCommand(rigtest.HasPrefix("curl"), func(a *rigtest.A) error {
			curl = append(curl, a.Ctx.Value(ctxKey{}))
			return nil
		})
		runner.AddCommandOutput(rigtest.Contains("curl"), "/usr/bin/curl\n")

		ctx := context.WithValue(context.Background(), ctxKey{}, "bound")
		d, ok := remotefs.WithContext(ctx, remotefs.NewPosixFS(runner)).(remotefs.Downloader)
		require.True(t, ok)
		require.NoError(t, d.DownloadURL("https://example.com/file", "/tmp/file"))
		require.Equal(t, []any{"bound"}, curl)
	})

	t.Run("a type embedding a rig filesystem is returned unchanged", func(t *testing.T) {
		var fsys remotefs.FS = embeddingFS{PosixFS: remotefs.NewPosixFS(rigtest.NewMockRunner())}
		require.Equal(t, fsys, remotefs.WithContext(context.Background(), fsys))
	})

	t.Run("other filesystems are returned unchanged", func(t *testing.T) {
		var fsys remotefs.FS = otherFS{}
		require.Equal(t, fsys, remotefs.WithContext(context.Background(), fsys))
	})
}

// otherFS is a filesystem that is not one of the package's implementations.
type otherFS struct {
	remotefs.FS
}

// embeddingFS wraps a PosixFS, as a caller overriding some of its methods would.
type embeddingFS struct {
	*remotefs.PosixFS
}
