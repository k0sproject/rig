package exec_test

import (
	"context"
	"errors"
	"io"
	"regexp"
	"testing"

	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/rigtest"
	"github.com/stretchr/testify/require"
)

func mockClient() *rigtest.MockClient {
	return rigtest.NewMockClient()
}

func TestSimpleExec(t *testing.T) {
	mc := mockClient()
	runner := exec.NewHostRunner(mc)
	mc.AddMockCommand(regexp.MustCompile("^true"), func(_ context.Context, _ io.Reader, _, _ io.Writer) error {
		return nil
	})
	mc.AddMockCommand(regexp.MustCompile("^false"), func(_ context.Context, _ io.Reader, _, _ io.Writer) error {
		return errors.New("foo")
	})

	require.NoError(t, runner.Exec("true"))
	require.Error(t, runner.Exec("false"))
}

func TestExecOutput(t *testing.T) {
	mc := mockClient()
	runner := exec.NewHostRunner(mc)
	mc.AddMockCommand(regexp.MustCompile("^foo"), func(_ context.Context, _ io.Reader, stdout, _ io.Writer) error {
		_, _ = stdout.Write([]byte("bar\n"))
		return nil
	})
	out, err := runner.ExecOutput("foo")
	require.NoError(t, err)
	require.Equal(t, "bar", out)
}

func TestStdinInput(t *testing.T) {
	mc := mockClient()
	runner := exec.NewHostRunner(mc)
	mc.AddMockCommand(regexp.MustCompile("^foo"), func(_ context.Context, stdin io.Reader, stdout, _ io.Writer) error {
		_, _ = io.Copy(stdout, stdin)
		return nil
	})
	out, err := runner.ExecOutput("foo", exec.StdinString("barbar"))
	require.NoError(t, err)
	require.Equal(t, "barbar", out)
}

func TestBackground(t *testing.T) {
	mc := mockClient()
	runner := exec.NewHostRunner(mc)
	mc.AddMockCommand(regexp.MustCompile("^foo"), func(_ context.Context, _ io.Reader, _, _ io.Writer) error {
		return errors.New("error from wait")
	})
	cmd, err := runner.StartBackground("foo")
	require.NoError(t, err)
	require.True(t, mc.ReceivedString("foo"))
	require.ErrorContains(t, cmd.Wait(), "error from wait")

}
