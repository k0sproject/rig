package rig

import (
	"context"
	"io"
	"testing"

	"github.com/creasty/defaults"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/sudo"
	"github.com/stretchr/testify/require"
)

type Host struct {
	Connection
}

type stubWaiter struct{}

func (s *stubWaiter) Wait() error { return nil }

type mockClient struct {
	commands []string
}

func (m *mockClient) Connect() error                             { return nil }
func (m *mockClient) Disconnect()                                {}
func (m *mockClient) Upload(_, _ string, _ ...exec.Option) error { return nil }
func (m *mockClient) IsWindows() bool                            { return false }
func (m *mockClient) ExecInteractive(_ string, _ io.Reader, _, _ io.Writer) error {
	return nil
}
func (m *mockClient) String() string    { return "mockclient" }
func (m *mockClient) Protocol() string  { return "null" }
func (m *mockClient) IPAddress() string { return "127.0.0.1" }
func (m *mockClient) IsConnected() bool { return true }
func (m *mockClient) StartProcess(ctx context.Context, cmd string, _ io.Reader, _, _ io.Writer) (exec.Waiter, error) {
	m.commands = append(m.commands, cmd)

	return &stubWaiter{}, nil
}

var stubSudofunc = func(in string) string {
	return "sudo-goes-here " + in
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
				Address: "127.0.0.1",
			},
		},
	}
	require.NoError(t, defaults.Set(&h))
	require.Equal(t, "SSH", h.Protocol())
	require.Equal(t, "127.0.0.1", h.Address())
}

func TestSudo(t *testing.T) {
	mc := mockClient{}
	h := Host{
		Connection: Connection{
			client: &mc,
			sudoRepo: sudo.NewRepository(func(c exec.SimpleRunner) exec.DecorateFunc {
				_ = c.Exec("sudocheck")
				return func(cmd string) string {
					return "sudo-goes-here " + cmd
				}
			}),
		},
	}
	h.Connect()

	require.NoError(t, h.Sudo().Exec("ls %s", "/tmp"))
	require.Contains(t, mc.commands, "sudo-goes-here ls /tmp")
	require.Contains(t, mc.commands, "sudocheck")
}
