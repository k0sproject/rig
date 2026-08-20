package rig_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/k0sproject/rig/v2"
	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/os"
	"github.com/k0sproject/rig/v2/protocol"
	"github.com/k0sproject/rig/v2/packagemanager"
	"github.com/k0sproject/rig/v2/remotefs"
	"github.com/k0sproject/rig/v2/rigtest"
	"github.com/stretchr/testify/assert"
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

// TestPosixShellImposed guards the fix for k0sproject/k0sctl#1135: rig's commands
// are POSIX, the remote user's login shell is not necessarily POSIX (fish, csh),
// and sshd runs commands through that login shell. Every command a client sends
// to a non-Windows host must therefore be handed to an explicit POSIX shell and
// must not rely on shell features the login shell may not have.
func TestPosixShellImposed(t *testing.T) {
	t.Run("plain command", func(t *testing.T) {
		conn := rigtest.NewMockConnection()
		client, err := rig.NewClient(rig.WithConnection(conn))
		require.NoError(t, err)
		require.NoError(t, client.Exec("echo hello"))
		rigtest.ReceivedEqual(t, conn, `/bin/sh -c -- 'echo hello'`)
	})

	t.Run("sudo command", func(t *testing.T) {
		conn := rigtest.NewMockConnection()
		forceSudo(conn)
		client, err := rig.NewClient(rig.WithConnection(conn))
		require.NoError(t, err)
		require.NoError(t, client.Sudo().Exec("echo hello"))

		// The inner shell is sudo's (sudo execs directly, so compound commands
		// need one), the outer shell replaces the login shell. Neither may be
		// applied twice.
		rigtest.ReceivedEqual(t, conn, `/bin/sh -c -- 'sudo -n -- /bin/sh -c -- '"'"'echo hello'"'"''`)
		// The sudo availability probe is the command that actually failed in
		// k0sctl#1135, so it must be wrapped as well.
		rigtest.ReceivedEqual(t, conn, `/bin/sh -c -- 'sudo -n -- /bin/sh -c -- true'`)
		// The root check is the very first command a client sends, and it is
		// also the one that resolves the host's OS. Wrapping must not depend on
		// a previous command having established that.
		rigtest.ReceivedEqual(t, conn, `/bin/sh -c -- '[ "$(id -u)" = 0 ]'`)

		// No command sent while setting sudo up may need a non-POSIX shell to
		// expand it either - the sudo and doas probes used to interpolate
		// "${SHELL-sh}", which is a syntax error in fish.
		for _, command := range conn.Commands() {
			assert.NotContains(t, command, "${", "commands must not rely on parameter expansion by the login shell")
		}
	})

	t.Run("windows host", func(t *testing.T) {
		conn := rigtest.NewMockConnection()
		conn.Windows = true
		client, err := rig.NewClient(rig.WithConnection(conn))
		require.NoError(t, err)
		require.NoError(t, client.Exec("echo hello"))
		rigtest.ReceivedEqual(t, conn, "cmd.exe /C echo hello")
	})
}

var errNotRoot = errors.New("not root")

// forceSudo makes the sudo registry deterministically select the "sudo"
// decorator against a mock connection. The mock otherwise succeeds on every
// command, which the root check would read as "already root" and both the sudo
// and doas probes would pass (leaving the choice to registry iteration order).
// It fails the UID0/root probe and the doas probe so only sudo qualifies.
func forceSudo(conn *rigtest.MockConnection) {
	conn.AddCommand(rigtest.Contains("id -u"), func(_ *rigtest.A) error { return errNotRoot })
	conn.AddCommand(rigtest.Contains("doas -n"), func(_ *rigtest.A) error { return errNotRoot })
}

// gateRecorder records the commands presented to a CommandGate and can be
// configured to allow or deny them.
type gateRecorder struct {
	mu    sync.Mutex
	seen  []string
	allow bool
}

func (g *gateRecorder) confirm(_, command string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.seen = append(g.seen, command)
	return g.allow
}

func (g *gateRecorder) commands() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.seen...)
}

func TestConfirmFuncGatesSudoCommandOnce(t *testing.T) {
	conn := rigtest.NewMockConnection()
	forceSudo(conn)
	conn.AddCommand(rigtest.Contains("systemctl restart nginx"), func(_ *rigtest.A) error { return nil })

	rec := &gateRecorder{allow: true}
	client, err := rig.NewClient(rig.WithConnection(conn), rig.WithConfirmFunc(rec.confirm))
	require.NoError(t, err)
	require.NoError(t, client.Connect(context.Background()))

	require.NoError(t, client.Sudo().ExecContext(context.Background(), "systemctl restart nginx"))

	seen := rec.commands()
	// Exactly one prompt: the sudo-availability probe is Ungated and must not
	// be presented, and the sudo wrapping must not cause a double prompt.
	require.Len(t, seen, 1, "expected exactly one gated command, got %v", seen)
	assert.Contains(t, seen[0], "systemctl restart nginx")
	assert.Contains(t, seen[0], "sudo -n", "gate must see the fully sudo-wrapped command")
}

func TestConfirmFuncSudoProbeIsUngated(t *testing.T) {
	conn := rigtest.NewMockConnection()
	forceSudo(conn)

	rec := &gateRecorder{allow: false} // deny everything
	client, err := rig.NewClient(rig.WithConnection(conn), rig.WithConfirmFunc(rec.confirm))
	require.NoError(t, err)
	require.NoError(t, client.Connect(context.Background()))

	// Denying the gate must not disable sudo: the probe runs Ungated, so the
	// sudo runner is a real sudo executor and rejection surfaces on the actual
	// command rather than silently downgrading to a non-sudo runner.
	err = client.Sudo().ExecContext(context.Background(), "id")
	require.Error(t, err)
	assert.ErrorIs(t, err, cmd.ErrCommandRejected)

	// All privilege probes (sudo/doas availability, UID-0) are Ungated, so the
	// only command the gate should have seen is the real, sudo-wrapped "id".
	// A leaked probe would show up as an extra entry here.
	seen := rec.commands()
	require.Len(t, seen, 1, "gate must see only the real command, not the ungated probes; got %v", seen)
	assert.Contains(t, seen[0], "id", "gate must see the real command")
	assert.NotContains(t, seen[0], "true", "the sudo probe (true) must not be presented to the gate")
}

func TestConfirmFuncRejectionBlocksExecution(t *testing.T) {
	conn := rigtest.NewMockConnection()
	conn.AddCommand(rigtest.Match("."), func(_ *rigtest.A) error { return nil })

	rec := &gateRecorder{allow: false}
	client, err := rig.NewClient(rig.WithConnection(conn), rig.WithConfirmFunc(rec.confirm))
	require.NoError(t, err)
	require.NoError(t, client.Connect(context.Background()))

	err = client.ExecContext(context.Background(), "rm -rf /data")
	require.Error(t, err)
	assert.ErrorIs(t, err, cmd.ErrCommandRejected)
	assert.NoError(t, conn.NotReceived(rigtest.Contains("rm -rf /data")), "rejected command must not reach the host")
}

func TestCommandGateContextErrorNotRejection(t *testing.T) {
	conn := rigtest.NewMockConnection()
	conn.AddCommand(rigtest.Match("."), func(_ *rigtest.A) error { return nil })

	// A gate that returns a context error (e.g. it honored cancellation while
	// prompting) must surface as cancellation, not as an explicit rejection.
	gate := cmd.CommandGateFunc(func(_ context.Context, _, _ string) error {
		return context.Canceled
	})
	client, err := rig.NewClient(rig.WithConnection(conn), rig.WithCommandGate(gate))
	require.NoError(t, err)
	require.NoError(t, client.Connect(context.Background()))

	err = client.ExecContext(context.Background(), "uptime")
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled, "context error must pass through")
	assert.NotErrorIs(t, err, cmd.ErrCommandRejected, "cancellation must not look like a rejection")
	assert.NoError(t, conn.NotReceived(rigtest.Contains("uptime")), "command must not reach the host")
}

func TestConfirmFuncNilDisablesGating(t *testing.T) {
	conn := rigtest.NewMockConnection()
	conn.AddCommand(rigtest.Match("."), func(_ *rigtest.A) error { return nil })

	client, err := rig.NewClient(rig.WithConnection(conn), rig.WithConfirmFunc(nil))
	require.NoError(t, err)
	require.NoError(t, client.Connect(context.Background()))

	// A nil confirm func must disable gating rather than panic on invocation.
	require.NoError(t, client.ExecContext(context.Background(), "echo hello"))
	require.NoError(t, conn.Received(rigtest.Contains("echo hello")))
}

func TestConfirmFuncGatesExecInteractive(t *testing.T) {
	conn := &interactiveConn{MockConnection: rigtest.NewMockConnection()}

	rec := &gateRecorder{allow: true}
	client, err := rig.NewClient(rig.WithConnection(conn), rig.WithConfirmFunc(rec.confirm))
	require.NoError(t, err)
	require.NoError(t, client.Connect(context.Background()))

	require.NoError(t, client.ExecInteractive(context.Background(), "top", nil, nil, nil))
	assert.Equal(t, "top", conn.receivedCmd, "allowed interactive command must start the session")
	assert.Contains(t, rec.commands(), "top", "the interactive command must be presented to the gate")
}

func TestConfirmFuncRejectsExecInteractive(t *testing.T) {
	conn := &interactiveConn{MockConnection: rigtest.NewMockConnection()}

	rec := &gateRecorder{allow: false}
	client, err := rig.NewClient(rig.WithConnection(conn), rig.WithConfirmFunc(rec.confirm))
	require.NoError(t, err)
	require.NoError(t, client.Connect(context.Background()))

	err = client.ExecInteractive(context.Background(), "top", nil, nil, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, cmd.ErrCommandRejected)
	assert.Empty(t, conn.receivedCmd, "rejected interactive command must not start the session")
}

func TestExecInteractiveCancelledContextSkipsGate(t *testing.T) {
	conn := &interactiveConn{MockConnection: rigtest.NewMockConnection()}

	rec := &gateRecorder{allow: true}
	client, err := rig.NewClient(rig.WithConnection(conn), rig.WithConfirmFunc(rec.confirm))
	require.NoError(t, err)
	require.NoError(t, client.Connect(context.Background()))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = client.ExecInteractive(ctx, "top", nil, nil, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled, "cancelled context must propagate")
	assert.NotErrorIs(t, err, cmd.ErrCommandRejected, "cancellation must not look like a rejection")
	assert.Empty(t, rec.commands(), "gate must not be consulted for a cancelled session")
	assert.Empty(t, conn.receivedCmd, "cancelled session must not start")
}

func TestConfirmFuncGatesFilesystemOps(t *testing.T) {
	conn := rigtest.NewMockConnection()
	forceSudo(conn)

	rec := &gateRecorder{allow: true}
	client, err := rig.NewClient(rig.WithConnection(conn), rig.WithConfirmFunc(rec.confirm))
	require.NoError(t, err)
	require.NoError(t, client.Connect(context.Background()))

	// Touch the filesystem service on the sudo client; its lazy detection runs
	// commands through the sudo runner, proving the gate propagates to FS.
	_, _ = client.Sudo().FS().Stat("/etc/hostname")

	var sawSudoWrapped bool
	for _, c := range rec.commands() {
		if strings.Contains(c, "sudo -n") {
			sawSudoWrapped = true
			break
		}
	}
	assert.True(t, sawSudoWrapped, "filesystem operations on a sudo client must be gated, got %v", rec.commands())
}
