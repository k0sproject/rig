package localhost_test

import (
	"bytes"
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
