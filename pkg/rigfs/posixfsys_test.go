package rigfs

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"testing"

	"al.essio.dev/pkg/shellescape"
	"github.com/k0sproject/rig/exec"
)

// mockConn is a simple test double for the connection interface.
type mockConn struct {
	outputs map[string]string // command → stdout output
	errors  map[string]error  // command → error (takes precedence over outputs)
	windows bool
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

// stubStatHelp pre-warms initStat on fsys by registering a GNU stat help response.
func stubStatHelp(conn *mockConn) {
	conn.outputs["stat --help 2>&1"] = "--format"
}

func TestReadDirEmptyDirectory(t *testing.T) {
	// Regression test for: find -print0 appends a trailing NUL, so
	// strings.Split(out, "\x00") for an empty dir yields ["dirname", ""]
	// (len==2) instead of ["dirname"] (len==1), causing ReadDir to call
	// multiStat("") instead of returning an empty slice.
	const dirName = "testdir"

	conn := newMockConn()
	stubStatHelp(conn)
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
	stubStatHelp(conn)
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
