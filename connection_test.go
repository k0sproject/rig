package rig

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type Host struct {
	Connection
}

func TestHostFunctions(t *testing.T) {
	h := Host{
		Connection: Connection{
			Localhost: &Localhost{
				Enabled: true,
			},
		},
	}

	require.NoError(t, h.Connect())
	require.Equal(t, "[local] localhost", h.String())
	require.True(t, h.IsConnected())
	h.Disconnect()
	require.False(t, h.IsConnected())
}
