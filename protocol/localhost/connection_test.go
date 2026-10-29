package localhost_test

import (
	"bytes"
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
