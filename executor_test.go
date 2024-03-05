package rig_test

import (
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/k0sproject/rig/cmd"
	"github.com/k0sproject/rig/rigtest"
	"github.com/stretchr/testify/require"
)

func TestSimpleExec(t *testing.T) {
	mr := rigtest.NewMockRunner()

	mr.AddCommand(rigtest.Equal("true"), func(a *rigtest.A) error { return nil })
	mr.AddCommand(rigtest.Equal("false"), func(a *rigtest.A) error { return errors.New("foo") })

	require.NoError(t, mr.Exec("true"))
	require.ErrorContains(t, mr.Exec("false"), "foo")
}

func TestWindowsShell(t *testing.T) {
	mr := rigtest.NewMockRunner()
	mr.Windows = true
	_ = mr.Exec("echo hello")
	rigtest.ReceivedEqual(t, mr, "cmd.exe /C echo hello", "commands should by default be run through cmd.exe")
	_ = mr.Exec("foo.exe foo")
	rigtest.ReceivedWithPrefix(t, mr, "foo.exe", "commands starting with *.exe should be run directly")
}

func TestPrintfErrors(t *testing.T) {
	mr := rigtest.NewMockRunner()
	args := []interface{}{"hello"}
	err := mr.Exec(fmt.Sprintf("echo %s %d", args...)) // intentional error
	require.ErrorIs(t, err, cmd.ErrInvalidCommand, "commands with printf errors should return ErrInvalidCommand")
	require.ErrorContains(t, err, "refusing", "commands with printf errors should return a helpful error message")
}

func TestExecOutput(t *testing.T) {
	mr := rigtest.NewMockRunner()
	mr.AddCommandOutput(rigtest.Equal("foo"), "bar\n")
	out, err := mr.ExecOutput("foo")
	require.NoError(t, err)
	require.Equal(t, "bar", out)
	out, err = mr.ExecOutput("foo", cmd.TrimOutput(false))
	require.NoError(t, err)
	require.Equal(t, "bar\n", out)
}

func TestStdinInput(t *testing.T) {
	mr := rigtest.NewMockRunner()
	var readN int64
	mr.AddCommand(rigtest.Equal("foo"), func(a *rigtest.A) error {
		readN, _ = io.Copy(a.Stdout, a.Stdin)
		return nil
	})
	out, err := mr.ExecOutput("foo", cmd.StdinString("barbar"))
	require.NoError(t, err)
	require.Equal(t, "barbar", out)
	require.Equal(t, 6, int(readN))
}

func TestBackground(t *testing.T) {
	mr := rigtest.NewMockRunner()
	mr.AddCommand(rigtest.Equal("foo"), func(_ *rigtest.A) error {
		return errors.New("error from mock wait")
	})
	cmd, err := mr.StartBackground("foo")
	require.NoError(t, err)
	rigtest.ReceivedEqual(t, mr, "foo")
	require.ErrorContains(t, cmd.Wait(), "error from mock wait")

}
