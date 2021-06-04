package rig

import (
	"bytes"
	"testing"

	"github.com/creasty/defaults"
	"github.com/k0sproject/rig/exec"
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

	require.NoError(t, defaults.Set(&h))
	require.NoError(t, h.Connect())
	require.Equal(t, "[local] localhost", h.String())
	require.True(t, h.IsConnected())
	require.Equal(t, "Local", h.Protocol())
	require.Equal(t, "127.0.0.1", h.Address())
	h.Disconnect()
	require.False(t, h.IsConnected())

	h = Host{
		Connection: Connection{
			SSH: &SSH{
				Address: "10.0.0.1",
			},
		},
	}
	require.NoError(t, defaults.Set(&h))
	require.Equal(t, "SSH", h.Protocol())
	require.Equal(t, "10.0.0.1", h.Address())
}

func TestOutputWriter(t *testing.T) {
	h := Host{
		Connection: Connection{
			Localhost: &Localhost{
				Enabled: true,
			},
		},
	}
	defaults.Set(&h)
	h.Connect()
	var buf []byte
	writer := bytes.NewBuffer(buf)
	require.NoError(t, h.Exec("echo hello world", exec.Writer(writer)))
	require.Equal(t, "hello world\n", writer.String())
}
