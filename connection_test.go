package rig

import (
	"bytes"
	"fmt"
	"io"
	"testing"

	"github.com/creasty/defaults"
	"github.com/k0sproject/rig/exec"
	"github.com/stretchr/testify/require"
)

type Host struct {
	Config
}

type mockClient struct {
	commands []string
}

func (m *mockClient) Connect() error                             { return nil }
func (m *mockClient) Disconnect()                                {}
func (m *mockClient) Upload(_, _ string, _ ...exec.Option) error { return nil }
func (m *mockClient) IsWindows() bool                            { return false }
func (m *mockClient) ExecInteractive(_ string) error             { return nil }
func (m *mockClient) String() string                             { return "mockclient" }
func (m *mockClient) Protocol() string                           { return "null" }
func (m *mockClient) IPAddress() string                          { return "127.0.0.1" }
func (m *mockClient) IsConnected() bool                          { return true }
func (m *mockClient) Exec(cmd string, opts ...exec.Option) error {
	o := exec.Build(opts...)
	cmd, err := o.Command(cmd)
	if err != nil {
		return err
	}

	m.commands = append(m.commands, cmd)

	return nil
}
func (m *mockClient) ExecStreams(cmd string, stdin io.ReadCloser, stdout, stderr io.Writer, opts ...exec.Option) (waiter, error) {
	return nil, fmt.Errorf("not implemented")
}

var stubSudofunc = func(in string) string {
	return "sudo-goes-here " + in
}

func TestHostFunctions(t *testing.T) {
	h := Host{
		Config: Config{
			LocalhostConfig: &LocalhostConfig{
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
		Config: Config{
			SSHConfig: &SSHConfig{
				Address: "127.0.0.1",
			},
		},
	}
	require.NoError(t, defaults.Set(&h))
	require.Equal(t, "SSH", h.Protocol())
	require.Equal(t, "127.0.0.1", h.Address())
}

func TestOutputWriter(t *testing.T) {
	h := Host{
		Config: Config{
			LocalhostConfig: &LocalhostConfig{
				Enabled: true,
			},
		},
	}
	require.NoError(t, defaults.Set(&h))
	require.NoError(t, h.Connect())
	var writer bytes.Buffer
	require.NoError(t, h.Exec("echo hello world", exec.Writer(&writer)))
	lt := "\n"
	if h.IsWindows() {
		lt = "\r\n"
	}
	require.Equal(t, "hello world"+lt, writer.String())
}

func TestGrouping(t *testing.T) {
	mc := mockClient{}
	h := Host{
		Config: Config{
			client:   &mc,
			sudofunc: stubSudofunc,
		},
	}

	opts, args := GroupParams(h, "ls", 1, exec.HideOutput(), exec.Sudo(h))
	require.Len(t, opts, 2)
	require.Len(t, args, 3)
}

func TestSudo(t *testing.T) {
	mc := mockClient{}
	h := Host{
		Config: Config{
			client:   &mc,
			sudofunc: stubSudofunc,
		},
	}

	require.NoError(t, h.Execf("ls %s", "/tmp", exec.Sudo(h)))
	require.Contains(t, mc.commands, "sudo-goes-here ls /tmp")
}
