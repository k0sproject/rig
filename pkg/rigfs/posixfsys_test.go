package rigfs

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"testing"

	"al.essio.dev/pkg/shellescape"
	"github.com/k0sproject/rig/exec"
	"github.com/stretchr/testify/assert"
)

// mockConn is a simple test double for the connection interface.
type mockConn struct {
	outputs map[string]string // command → stdout output
	errors  map[string]error  // command → error (takes precedence over outputs)
	windows bool
	strict  bool
}

func newMockConn() *mockConn {
	return &mockConn{
		outputs: make(map[string]string),
		errors:  make(map[string]error),
	}
}

func (m *mockConn) IsWindows() bool { return m.windows }

func (m *mockConn) Exec(cmd string, _ ...exec.Option) error {
	if err, ok := m.errors[cmd]; ok {
		return err
	}
	if m.strict {
		if _, ok := m.outputs[cmd]; !ok {
			return fmt.Errorf("unexpected command: %s", cmd)
		}
	}
	return nil
}

func (m *mockConn) ExecOutput(cmd string, _ ...exec.Option) (string, error) {
	if err, ok := m.errors[cmd]; ok {
		return "", err
	}
	if out, ok := m.outputs[cmd]; ok {
		return out, nil
	}
	return "", fmt.Errorf("unexpected command: %s", cmd)
}

func (m *mockConn) ExecStreams(cmd string, _ io.ReadCloser, _ io.Writer, _ io.Writer, _ ...exec.Option) (exec.Waiter, error) {
	return nil, fmt.Errorf("ExecStreams not implemented in mockConn: %s", cmd)
}

// findCmd returns the exact find command that ReadDir sends for the given directory name.
func findCmd(name string) string {
	return fmt.Sprintf("find %s -maxdepth 1 -print0", shellescape.Quote(name))
}

// stubInitStat pre-warms initStat on fsys by registering a GNU response.
func stubInitStat(conn *mockConn) {
	conn.outputs["stat -c %n /"] = "/"
}

func TestReadDirEmptyDirectory(t *testing.T) {
	// Regression test for: find -print0 appends a trailing NUL, so
	// strings.Split(out, "\x00") for an empty dir yields ["dirname", ""]
	// (len==2) instead of ["dirname"] (len==1), causing ReadDir to call
	// multiStat("") instead of returning an empty slice.
	const dirName = "testdir"

	conn := newMockConn()
	stubInitStat(conn)
	// find -print0 output for an empty directory: just the directory name
	// followed by the mandatory trailing NUL byte.
	conn.outputs[findCmd(dirName)] = dirName + "\x00"

	fsys := NewPosixFsys(conn)

	entries, err := fsys.ReadDir(dirName)
	if err != nil {
		t.Fatalf("ReadDir on empty directory returned unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("ReadDir on empty directory returned %d entries, want 0", len(entries))
	}
}

func TestReadDirNonExistentDirectory(t *testing.T) {
	// find returns nothing (or just a trailing NUL) for a path that does not
	// exist; ReadDir must return fs.ErrNotExist in that case.
	const dirName = "no-such-dir"

	conn := newMockConn()
	stubInitStat(conn)
	// Simulate find producing only a trailing NUL (empty output).
	conn.outputs[findCmd(dirName)] = "\x00"

	fsys := NewPosixFsys(conn)

	_, err := fsys.ReadDir(dirName)
	if err == nil {
		t.Fatal("ReadDir on non-existent directory returned nil error, want fs.ErrNotExist")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("ReadDir returned %v, want an error wrapping fs.ErrNotExist", err)
	}
	var pe *fs.PathError
	if !errors.As(err, &pe) || pe.Path != dirName {
		t.Fatalf("ReadDir returned %v, want *fs.PathError with Path=%q", err, dirName)
	}
}

func TestPosixInitStat(t *testing.T) {
	// initStat selects between GNU and BSD stat by inspecting stat's capabilities.
	// GNU mode tests and uses "stat -c", BSD mode tests "stat -s" and uses "stat -f".
	cases := []struct {
		name  string
		probe string
		query string
	}{
		{
			"GNU",
			"stat -c %n /",
			`env -i PATH="$PATH" LC_ALL=C stat -c '%#f %s %.9Y //%n//' -- /tmp/file 2> /dev/null`,
		},
		{
			"BSD",
			"stat -s /",
			`env -i PATH="$PATH" LC_ALL=C stat -f '%#p %z %Fm //%N//' -- /tmp/file 2> /dev/null`,
		},
		{"unknown", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conn := newMockConn()
			conn.strict = true
			if tc.probe != "" {
				conn.outputs[tc.probe] = ""
			}
			if tc.query != "" {
				conn.outputs[tc.query] = `0x81a4 0 0.0 ///tmp/file//`
			}

			fsys := NewPosixFsys(conn)
			stat, err := fsys.Stat("/tmp/file")

			if tc.probe == "" {
				assert.ErrorContains(t, err, "unsupported stat implementation")
			} else {
				assert.NoError(t, err)
				assert.Equal(t, "file", stat.Name())
				assert.True(t, stat.Mode().IsRegular())
			}
		})
	}
}
