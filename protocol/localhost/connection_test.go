package localhost_test

import (
	"bytes"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"github.com/k0sproject/rig/v2/protocol"
	"github.com/k0sproject/rig/v2/protocol/localhost"
	"github.com/stretchr/testify/require"
)

func TestExecInteractiveEmptyCommand(t *testing.T) {
	conn, err := localhost.NewConnection()
	require.NoError(t, err)

	for _, cmd := range []string{"   ", `""`, `'' arg`} {
		t.Run(cmd, func(t *testing.T) {
			var out bytes.Buffer
			err := conn.ExecInteractive(t.Context(), cmd, &bytes.Buffer{}, &out, &out)
			require.ErrorIs(t, err, protocol.ErrValidationFailed)
		})
	}
}

func TestExecInteractiveCopiesAllOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires /bin/sh")
	}
	conn, err := localhost.NewConnection()
	require.NoError(t, err)

	const size = 1 << 20
	var stdout, stderr bytes.Buffer
	script := strings.NewReader("head -c 1048576 /dev/zero; echo err >&2\n")
	require.NoError(t, conn.ExecInteractive(t.Context(), "/bin/sh", script, &stdout, &stderr))
	require.Equal(t, size, stdout.Len())
	require.Equal(t, "err\n", stderr.String())
}

func TestExecInteractiveStartFailure(t *testing.T) {
	conn, err := localhost.NewConnection()
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	err = conn.ExecInteractive(t.Context(), "/rig-test-no-such-binary", strings.NewReader("input"), &stdout, &stderr)
	require.ErrorContains(t, err, "failed to start process")
	require.Zero(t, stdout.Len())
	require.Zero(t, stderr.Len())
}

func TestExecInteractiveExitStatus(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /usr/bin/true and /usr/bin/false")
	}

	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = devNull.Close() })

	conn, err := localhost.NewConnection()
	require.NoError(t, err)

	require.NoError(t, conn.ExecInteractive(t.Context(), "/usr/bin/true", devNull, devNull, devNull))

	err = conn.ExecInteractive(t.Context(), "/usr/bin/false", devNull, devNull, devNull)
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	require.Equal(t, 1, exitErr.ExitCode())
}
