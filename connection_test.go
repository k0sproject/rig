package rig

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"testing"

	"github.com/creasty/defaults"
	"github.com/k0sproject/rig/exec"
	"github.com/stretchr/testify/require"
)

type Host struct {
	Connection
}

type mockClient struct {
	commands []string
}

func (m *mockClient) Connect() error                                            { return nil }
func (m *mockClient) Disconnect()                                               {}
func (m *mockClient) Upload(_, _ string, _ fs.FileMode, _ ...exec.Option) error { return nil }
func (m *mockClient) IsWindows() bool                                           { return false }
func (m *mockClient) ExecInteractive(_ string) error                            { return nil }
func (m *mockClient) String() string                                            { return "mockclient" }
func (m *mockClient) Protocol() string                                          { return "null" }
func (m *mockClient) IPAddress() string                                         { return "127.0.0.1" }
func (m *mockClient) IsConnected() bool                                         { return true }
func (m *mockClient) Exec(cmd string, opts ...exec.Option) error {
	o := exec.Build(opts...)
	cmd, err := o.Command(cmd)
	if err != nil {
		return err
	}

	m.commands = append(m.commands, cmd)

	return nil
}

func (m *mockClient) ExecStreams(cmd string, stdin io.ReadCloser, stdout, stderr io.Writer, opts ...exec.Option) (exec.Waiter, error) {
	return nil, fmt.Errorf("not implemented")
}

var stubSudofunc = func(in string) string {
	return "sudo-goes-here " + in
}

type windowsMockClient struct {
	execErr error
}

func (m *windowsMockClient) Connect() error                        { return nil }
func (m *windowsMockClient) Disconnect()                           {}
func (m *windowsMockClient) IsWindows() bool                       { return true }
func (m *windowsMockClient) ExecInteractive(_ string) error        { return nil }
func (m *windowsMockClient) String() string                        { return "windows-mock" }
func (m *windowsMockClient) Protocol() string                      { return "winrm" }
func (m *windowsMockClient) IPAddress() string                     { return "192.0.2.1" }
func (m *windowsMockClient) IsConnected() bool                     { return true }
func (m *windowsMockClient) Exec(_ string, _ ...exec.Option) error { return m.execErr }
func (m *windowsMockClient) ExecStreams(_ string, _ io.ReadCloser, _, _ io.Writer, _ ...exec.Option) (exec.Waiter, error) {
	return nil, fmt.Errorf("not implemented")
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

func TestOutputWriter(t *testing.T) {
	h := Host{
		Connection: Connection{
			Localhost: &Localhost{
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
	h := Host{Connection: Connection{sudofunc: stubSudofunc}}
	h.setClient(&mc)

	opts, args := GroupParams(h, "ls", 1, exec.HideOutput(), exec.Sudo(&h))
	require.Len(t, opts, 2)
	require.Len(t, args, 3)
}

func TestDiscoverSudoWindows(t *testing.T) {
	t.Run("elevated session sets sudofunc", func(t *testing.T) {
		mc := &windowsMockClient{}
		h := Host{}
		h.setClient(mc)
		h.discoverSudo()
		result, err := h.Sudo("whoami")
		require.NoError(t, err)
		require.Equal(t, "whoami", result)
	})

	t.Run("non-elevated session returns ErrSudoRequired", func(t *testing.T) {
		mc := &windowsMockClient{execErr: fmt.Errorf("access denied")}
		h := Host{}
		h.setClient(mc)
		h.discoverSudo()
		_, err := h.Sudo("whoami")
		require.ErrorIs(t, err, ErrSudoRequired)
		require.ErrorContains(t, err, "administrator privileges")
	})
}

func TestSudo(t *testing.T) {
	mc := mockClient{}
	h := Host{Connection: Connection{sudofunc: stubSudofunc}}
	h.setClient(&mc)

	require.NoError(t, h.Execf("ls %s", "/tmp", exec.Sudo(&h)))
	require.Contains(t, mc.commands, "sudo-goes-here ls /tmp")
}

// pausingClient blocks in IsConnected until released, so that a test can
// disconnect between a command's connection check and its use of the client.
type pausingClient struct {
	mockClient
	entered chan struct{}
	release chan struct{}
}

func (p *pausingClient) IsConnected() bool {
	p.entered <- struct{}{}
	<-p.release
	return true
}

// TestDisconnectConcurrently runs under -race to check that disconnecting from
// another goroutine does not race a command still being run on the
// connection, and that the command keeps the client it checked.
func TestDisconnectConcurrently(t *testing.T) {
	pc := &pausingClient{entered: make(chan struct{}), release: make(chan struct{})}
	h := &Host{}
	h.setClient(pc)

	execErr := make(chan error, 1)
	go func() { execErr <- h.Exec("true") }()
	<-pc.entered
	h.Disconnect()
	close(pc.release)
	require.NoError(t, <-execErr)
	require.Equal(t, []string{"true"}, pc.commands, "the command runs on the client it checked")

	require.False(t, h.IsConnected())
	require.ErrorIs(t, h.Exec("true"), ErrNotConnected)
	_, err := h.ExecStreams("true", nil, nil, nil)
	require.ErrorIs(t, err, ErrNotConnected)
	require.ErrorIs(t, h.ExecInteractive("true"), ErrNotConnected)
}

func TestSudoSudo(t *testing.T) {
	for _, tt := range []struct {
		name string
		cmd  string
		want string
	}{
		{
			name: "simple command",
			cmd:  "ls /tmp",
			want: `sudo -n -- "${SHELL:-sh}" -c 'ls /tmp'`,
		},
		{
			name: "compound command with subshell groups",
			cmd:  "(systemctl start k0sworker) || (rc-service k0sworker start)",
			want: `sudo -n -- "${SHELL:-sh}" -c '(systemctl start k0sworker) || (rc-service k0sworker start)'`,
		},
		{
			name: "pipeline",
			cmd:  "cat /etc/passwd | grep root",
			want: `sudo -n -- "${SHELL:-sh}" -c 'cat /etc/passwd | grep root'`,
		},
		{
			name: "leading environment assignment",
			cmd:  "FOO=bar printenv FOO",
			want: `sudo -n -- "${SHELL:-sh}" -c 'FOO=bar printenv FOO'`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, sudoSudo(tt.cmd))
		})
	}
}
