package rig_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/k0sproject/rig/v2"
	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/os"
	"github.com/k0sproject/rig/v2/protocol"
	"github.com/k0sproject/rig/v2/packagemanager"
	"github.com/k0sproject/rig/v2/remotefs"
	"github.com/k0sproject/rig/v2/rigtest"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
)

func TestClientWithConnectionFactory(t *testing.T) {
	cc := &rig.CompositeConfig{
		Localhost: true,
	}
	conn, err := rig.NewClient(
		rig.WithConnectionFactory(cc),
	)
	require.NoError(t, err)
	require.NotNil(t, conn)

	require.NoError(t, conn.Connect(context.Background()))

	out, err := conn.ExecOutput("echo hello")
	require.NoError(t, err)
	require.Equal(t, "hello", out)
}

func TestClient(t *testing.T) {
	conn := rigtest.NewMockConnection()
	conn.AddCommandOutput(rigtest.Match("echo hello"), "hello")

	client, err := rig.NewClient(rig.WithConnection(conn))
	require.NoError(t, err)

	require.NoError(t, client.Connect(context.Background()))

	out, err := client.ExecOutput("echo hello")
	require.NoError(t, err)
	require.Equal(t, "hello", out)
}

func TestClientLogging(t *testing.T) {
	conn := rigtest.NewMockConnection()
	conn.AddCommandOutput(rigtest.Match("echo hello"), "hello")

	logger := &rigtest.MockLogger{}
	client, err := rig.NewClient(rig.WithConnection(conn), rig.WithLogger(logger))
	require.NoError(t, err)

	require.NoError(t, client.Connect(context.Background()))

	_, _ = client.ExecOutput("echo hello")

	t.Log(logger.Messages())
}

func TestClientFSErrorFallback(t *testing.T) {
	conn := rigtest.NewMockConnection()
	mockErr := errors.New("mock fs error")

	client, err := rig.NewClient(
		rig.WithConnection(conn),
		rig.WithRemoteFSProvider(func(_ cmd.Runner) (remotefs.FS, error) {
			return nil, mockErr
		}),
	)
	require.NoError(t, err)
	require.NoError(t, client.Connect(context.Background()))

	fs := client.FS()
	require.NotNil(t, fs)

	_, err = fs.Open("test")
	require.Error(t, err)

	_, err = client.RemoteFSProvider.FS()
	require.ErrorIs(t, err, mockErr)
}

func TestClientPackageManagerErrorFallback(t *testing.T) {
	conn := rigtest.NewMockConnection()
	mockErr := errors.New("mock pm error")

	client, err := rig.NewClient(
		rig.WithConnection(conn),
		rig.WithPackageManagerProvider(func(_ cmd.ContextRunner) (packagemanager.PackageManager, error) {
			return nil, mockErr
		}),
	)
	require.NoError(t, err)
	require.NoError(t, client.Connect(context.Background()))

	pm := client.PackageManager()
	require.NotNil(t, pm)

	err = pm.Install(context.Background(), "test-package")
	require.ErrorIs(t, err, mockErr)
}

func TestClientReconnect(t *testing.T) {
	conn := rigtest.NewMockConnection()
	conn.AddCommandOutput(rigtest.Match("echo hello"), "hello")

	client, err := rig.NewClient(rig.WithConnection(conn))
	require.NoError(t, err)

	require.NoError(t, client.Connect(context.Background()))
	require.True(t, client.IsConnected())

	out, err := client.ExecOutput("echo hello")
	require.NoError(t, err)
	require.Equal(t, "hello", out)

	client.Disconnect()
	require.False(t, client.IsConnected())

	require.NoError(t, client.Connect(context.Background()))
	require.True(t, client.IsConnected())

	out, err = client.ExecOutput("echo hello")
	require.NoError(t, err)
	require.Equal(t, "hello", out)
}

func TestClientProtocolName(t *testing.T) {
	conn := rigtest.NewMockConnection()
	client, err := rig.NewClient(rig.WithConnection(conn))
	require.NoError(t, err)

	require.Equal(t, "mock", client.Protocol())
	require.Equal(t, "mock", client.ProtocolName())
}

type testConfig struct {
	Hosts []*testHost `yaml:"hosts"`
}

type testHost struct {
	ClientConfig rig.CompositeConfig `yaml:"-,inline"`
	*rig.Client
}

func (th *testHost) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type rawTestHost testHost
	h := (*rawTestHost)(th)
	if err := unmarshal(h); err != nil {
		return err
	}
	conn, err := rig.NewClient(rig.WithConnectionFactory(&h.ClientConfig))
	if err != nil {
		return err
	}
	h.Client = conn
	return nil
}

func TestConnectionUnmarshal(t *testing.T) {
	hostConfig := map[string]any{
		"localhost": true,
	}
	mainConfig := map[string]any{
		"hosts": []map[string]any{hostConfig},
	}
	yamlContent, err := yaml.Marshal(mainConfig)
	require.NoError(t, err)

	testConfig := &testConfig{}
	require.NoError(t, yaml.Unmarshal(yamlContent, testConfig))
	require.Len(t, testConfig.Hosts, 1)
	conn := testConfig.Hosts[0]

	require.NoError(t, conn.Connect(context.Background()))

	require.Equal(t, "Local", conn.Protocol())

	require.NoError(t, conn.Connect(context.Background()))

	out, err := conn.ExecOutput("echo hello")
	require.NoError(t, err)
	require.Equal(t, "hello", out)
}

type testConfigConfigured struct {
	Hosts []*testHostConfigured `yaml:"hosts"`
}

type testHostConfigured struct {
	rig.ClientWithConfig `yaml:"-,inline"`
}

func TestConfiguredConnectionUnmarshal(t *testing.T) {
	hostConfig := map[string]any{
		"localhost": true,
	}
	mainConfig := map[string]any{
		"hosts": []map[string]any{hostConfig},
	}
	yamlContent, err := yaml.Marshal(mainConfig)
	require.NoError(t, err)

	testConfig := &testConfigConfigured{}
	require.NoError(t, yaml.Unmarshal(yamlContent, testConfig))
	require.Len(t, testConfig.Hosts, 1)
	conn := testConfig.Hosts[0]

	require.NoError(t, conn.Connect(context.Background()))

	require.Equal(t, "Local", conn.Protocol())

	require.NoError(t, conn.Connect(context.Background()))

	out, err := conn.ExecOutput("echo hello")
	require.NoError(t, err)
	require.Equal(t, "hello", out)
}

func TestWithOSIDOverride(t *testing.T) {
	detectedRelease := &os.Release{
		ID:     "detected-os",
		Name:   "Detected OS",
		IDLike: []string{"family"},
	}
	releaseProvider := func(_ cmd.SimpleRunner) (*os.Release, error) {
		return detectedRelease, nil
	}

	t.Run("override after provider", func(t *testing.T) {
		conn := rigtest.NewMockConnection()
		client, err := rig.NewClient(
			rig.WithConnection(conn),
			rig.WithOSReleaseProvider(releaseProvider),
			rig.WithOSIDOverride("override-id"),
		)
		require.NoError(t, err)
		require.NoError(t, client.Connect(context.Background()))

		release, err := client.OS()
		require.NoError(t, err)
		require.Equal(t, "override-id", release.ID)
		require.Equal(t, "Detected OS", release.Name)
		require.Equal(t, []string{"family"}, release.IDLike)
	})

	t.Run("override before provider", func(t *testing.T) {
		conn := rigtest.NewMockConnection()
		client, err := rig.NewClient(
			rig.WithConnection(conn),
			rig.WithOSIDOverride("override-id"),
			rig.WithOSReleaseProvider(releaseProvider),
		)
		require.NoError(t, err)
		require.NoError(t, client.Connect(context.Background()))

		release, err := client.OS()
		require.NoError(t, err)
		require.Equal(t, "override-id", release.ID)
		require.Equal(t, "Detected OS", release.Name)
		require.Equal(t, []string{"family"}, release.IDLike)
	})

	t.Run("nil release from provider", func(t *testing.T) {
		conn := rigtest.NewMockConnection()
		client, err := rig.NewClient(
			rig.WithConnection(conn),
			rig.WithOSReleaseProvider(func(_ cmd.SimpleRunner) (*os.Release, error) {
				return nil, nil
			}),
			rig.WithOSIDOverride("override-id"),
		)
		require.NoError(t, err)
		require.NoError(t, client.Connect(context.Background()))

		_, err = client.OS()
		require.Error(t, err)
	})
}

// TestConfiguredConnectionConnectOptsApplied is a regression test verifying that
// options passed to Connect() after YAML unmarshal are actually applied.
// Previously, UnmarshalYAML called Setup() eagerly, causing a subsequent
// Connect(ctx, opts...) to skip Setup() and silently ignore those options.
func TestConfiguredConnectionConnectOptsApplied(t *testing.T) {
	hostConfig := map[string]any{
		"localhost": true,
	}
	mainConfig := map[string]any{
		"hosts": []map[string]any{hostConfig},
	}
	yamlContent, err := yaml.Marshal(mainConfig)
	require.NoError(t, err)

	testConfig := &testConfigConfigured{}
	require.NoError(t, yaml.Unmarshal(yamlContent, testConfig))
	require.Len(t, testConfig.Hosts, 1)
	conn := testConfig.Hosts[0]

	mock := rigtest.NewMockRunner()
	mock.AddCommand(rigtest.Equal("echo hello"), func(a *rigtest.A) error { return nil })

	require.NoError(t, conn.Connect(context.Background(), rig.WithRunner(mock)))

	_, err = conn.ExecOutput("echo hello")
	require.NoError(t, err)

	rigtest.ReceivedEqual(t, mock, "echo hello", "WithRunner option passed to Connect was not applied")
}

func TestClientRebootNilConnection(t *testing.T) {
	conn := rigtest.NewMockConnection()
	client, err := rig.NewClient(rig.WithConnection(conn))
	require.NoError(t, err)
	require.NoError(t, client.Connect(context.Background()))

	// Clone with a nil connection forces an uninitialized connection state.
	uninitialized := client.Clone(rig.WithConnection(nil))
	err = uninitialized.Reboot(context.Background())
	require.Error(t, err)
	require.ErrorIs(t, err, protocol.ErrNonRetryable)
}

func TestClientRebootFSErrorWrapped(t *testing.T) {
	mockErr := errors.New("reboot failed on remote")
	conn := rigtest.NewMockConnection()
	client, err := rig.NewClient(
		rig.WithConnection(conn),
		rig.WithRemoteFSProvider(func(_ cmd.Runner) (remotefs.FS, error) {
			return nil, mockErr
		}),
	)
	require.NoError(t, err)
	require.NoError(t, client.Connect(context.Background()))

	err = client.Reboot(context.Background())
	require.Error(t, err)
	require.ErrorIs(t, err, mockErr)
	require.Contains(t, err.Error(), "reboot:")
}

// interactiveConn embeds MockConnection and additionally implements InteractiveExecer
// so that Client.ExecInteractive forwards through to it.
type interactiveConn struct {
	*rigtest.MockConnection
	receivedCtx context.Context
	receivedCmd string
	returnErr   error
}

func (c *interactiveConn) ExecInteractive(ctx context.Context, cmd string, _ io.Reader, _ io.Writer, _ io.Writer) error {
	c.receivedCtx = ctx
	c.receivedCmd = cmd
	return c.returnErr
}

func TestClientExecInteractiveForwardsContext(t *testing.T) {
	conn := &interactiveConn{MockConnection: rigtest.NewMockConnection()}
	client, err := rig.NewClient(rig.WithConnection(conn))
	require.NoError(t, err)
	require.NoError(t, client.Connect(context.Background()))

	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "marker")

	require.NoError(t, client.ExecInteractive(ctx, "echo hello", nil, nil, nil))
	require.Equal(t, "echo hello", conn.receivedCmd)
	require.Equal(t, "marker", conn.receivedCtx.Value(ctxKey{}))
}

func TestClientExecInteractiveCancelledContext(t *testing.T) {
	conn := &interactiveConn{
		MockConnection: rigtest.NewMockConnection(),
		returnErr:      context.Canceled,
	}
	client, err := rig.NewClient(rig.WithConnection(conn))
	require.NoError(t, err)
	require.NoError(t, client.Connect(context.Background()))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = client.ExecInteractive(ctx, "sleep 60", nil, nil, nil)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
}

func TestClientExecInteractiveNotSupported(t *testing.T) {
	// MockConnection does not implement InteractiveExecer, so the client should
	// return an error indicating that interactive exec is unsupported.
	conn := rigtest.NewMockConnection()
	client, err := rig.NewClient(rig.WithConnection(conn))
	require.NoError(t, err)
	require.NoError(t, client.Connect(context.Background()))

	err = client.ExecInteractive(context.Background(), "sh", nil, nil, nil)
	require.Error(t, err)
	require.ErrorContains(t, err, "interactive")
}
