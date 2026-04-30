package cmd_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/rigtest"
	"github.com/stretchr/testify/require"
)

func TestProcRun(t *testing.T) {
	mr := rigtest.NewMockRunner()
	mr.AddCommandSuccess(rigtest.Equal("true"))
	mr.AddCommandFailure(rigtest.Equal("false"), errors.New("exit 1"))

	require.NoError(t, mr.Proc("true").Run(context.Background()))
	require.Error(t, mr.Proc("false").Run(context.Background()))
}

func TestProcStdout(t *testing.T) {
	mr := rigtest.NewMockRunner()
	mr.AddCommand(rigtest.Equal("hello"), func(a *rigtest.A) error {
		fmt.Fprint(a.Stdout, "world")
		return nil
	})

	var buf bytes.Buffer
	proc := mr.Proc("hello")
	proc.Stdout = &buf
	require.NoError(t, proc.Run(context.Background()))
	require.Equal(t, "world", buf.String())
}

func TestProcStderr(t *testing.T) {
	mr := rigtest.NewMockRunner()
	mr.AddCommand(rigtest.Equal("cmd"), func(a *rigtest.A) error {
		fmt.Fprint(a.Stderr, "oops")
		return errors.New("failed")
	})

	var errbuf bytes.Buffer
	proc := mr.Proc("cmd")
	proc.Stderr = &errbuf
	require.Error(t, proc.Run(context.Background()))
	require.Equal(t, "oops", errbuf.String())
}

func TestProcStdin(t *testing.T) {
	mr := rigtest.NewMockRunner()
	mr.AddCommand(rigtest.Equal("cat"), func(a *rigtest.A) error {
		_, err := io.Copy(a.Stdout, a.Stdin)
		return err
	})

	var buf bytes.Buffer
	proc := mr.Proc("cat")
	proc.Stdin = strings.NewReader("ping")
	proc.Stdout = &buf
	require.NoError(t, proc.Run(context.Background()))
	require.Equal(t, "ping", buf.String())
}

func TestProcStart(t *testing.T) {
	mr := rigtest.NewMockRunner()
	mr.AddCommandSuccess(rigtest.Equal("daemon"))

	waiter, err := mr.Proc("daemon").Start(context.Background())
	require.NoError(t, err)
	require.NoError(t, waiter.Wait())
}

func TestProcOptsPassedThrough(t *testing.T) {
	mr := rigtest.NewMockRunner()
	mr.AddCommandSuccess(rigtest.Equal("cmd"))

	proc := mr.Proc("cmd")
	require.NoError(t, proc.Run(context.Background(), cmd.HideOutput()))
}

func TestProcNilFieldsSkipped(t *testing.T) {
	mr := rigtest.NewMockRunner()
	mr.AddCommand(rigtest.Equal("cmd"), func(a *rigtest.A) error {
		require.Nil(t, a.Stdin, "nil Stdin should not be wired")
		return nil
	})
	require.NoError(t, mr.Proc("cmd").Run(context.Background()))
}
