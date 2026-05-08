package rig

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"testing"

	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/initsystem"
	"github.com/k0sproject/rig/v2/rigtest"
	"github.com/stretchr/testify/require"
)

// mockEnvManager is a ServiceManager that also implements ServiceEnvironmentManager.
type mockEnvManager struct {
	envPath    string
	envContent string
}

func (m *mockEnvManager) StartService(_ context.Context, _ cmd.ContextRunner, _ string) error {
	return nil
}

func (m *mockEnvManager) StopService(_ context.Context, _ cmd.ContextRunner, _ string) error {
	return nil
}

func (m *mockEnvManager) ServiceScriptPath(_ context.Context, _ cmd.ContextRunner, _ string) (string, error) {
	return "", nil
}

func (m *mockEnvManager) EnableService(_ context.Context, _ cmd.ContextRunner, _ string) error {
	return nil
}

func (m *mockEnvManager) DisableService(_ context.Context, _ cmd.ContextRunner, _ string) error {
	return nil
}

func (m *mockEnvManager) ServiceIsRunning(_ context.Context, _ cmd.ContextRunner, _ string) bool {
	return false
}

func (m *mockEnvManager) ServiceEnvironmentPath(_ context.Context, _ cmd.ContextRunner, _ string) (string, error) {
	return m.envPath, nil
}

func (m *mockEnvManager) ServiceEnvironmentContent(env map[string]string) string {
	return m.envContent
}

// mockReloadEnvManager adds DaemonReload support to mockEnvManager.
type mockReloadEnvManager struct {
	mockEnvManager
	reloaded bool
}

func (m *mockReloadEnvManager) DaemonReload(_ context.Context, _ cmd.ContextRunner) error {
	m.reloaded = true
	return nil
}

// mockBasicManager is a ServiceManager without env or reload support.
type mockBasicManager struct{}

func (m *mockBasicManager) StartService(_ context.Context, _ cmd.ContextRunner, _ string) error {
	return nil
}

func (m *mockBasicManager) StopService(_ context.Context, _ cmd.ContextRunner, _ string) error {
	return nil
}

func (m *mockBasicManager) ServiceScriptPath(_ context.Context, _ cmd.ContextRunner, _ string) (string, error) {
	return "", nil
}

func (m *mockBasicManager) EnableService(_ context.Context, _ cmd.ContextRunner, _ string) error {
	return nil
}

func (m *mockBasicManager) DisableService(_ context.Context, _ cmd.ContextRunner, _ string) error {
	return nil
}

func (m *mockBasicManager) ServiceIsRunning(_ context.Context, _ cmd.ContextRunner, _ string) bool {
	return false
}

// mockFS captures WriteFile and MkdirAll calls.
type mockFS struct {
	mkdirAllPath string
	writtenPath  string
	writtenData  []byte
}

func (f *mockFS) MkdirAll(path string, _ fs.FileMode) error {
	f.mkdirAllPath = path
	return nil
}

func (f *mockFS) WriteFile(path string, data []byte, _ fs.FileMode) error {
	f.writtenPath = path
	f.writtenData = data
	return nil
}

func TestServiceSetEnvironment(t *testing.T) {
	ctx := context.Background()
	env := map[string]string{"FOO": "bar"}

	t.Run("with reloader", func(t *testing.T) {
		mgr := &mockReloadEnvManager{
			mockEnvManager: mockEnvManager{
				envPath:    "/etc/systemd/system/mysvc.service.d/env.conf",
				envContent: "[Service]\nEnvironment='FOO=bar'\n",
			},
		}
		mfs := &mockFS{}
		svc := &Service{
			runner:  rigtest.NewMockRunner(),
			name:    "mysvc",
			initsys: mgr,
			fs:      mfs,
		}

		require.NoError(t, svc.SetEnvironment(ctx, env))
		require.Equal(t, "/etc/systemd/system/mysvc.service.d", mfs.mkdirAllPath)
		require.Equal(t, "/etc/systemd/system/mysvc.service.d/env.conf", mfs.writtenPath)
		require.Equal(t, mgr.envContent, string(mfs.writtenData))
		require.True(t, mgr.reloaded)
	})

	t.Run("without reloader", func(t *testing.T) {
		mgr := &mockEnvManager{
			envPath:    "/etc/conf.d/mysvc",
			envContent: "export FOO=bar\n",
		}
		mfs := &mockFS{}
		svc := &Service{
			runner:  rigtest.NewMockRunner(),
			name:    "mysvc",
			initsys: mgr,
			fs:      mfs,
		}

		require.NoError(t, svc.SetEnvironment(ctx, env))
		require.Equal(t, "/etc/conf.d", mfs.mkdirAllPath)
		require.Equal(t, "/etc/conf.d/mysvc", mfs.writtenPath)
	})

	t.Run("env manager not supported", func(t *testing.T) {
		svc := &Service{
			runner:  rigtest.NewMockRunner(),
			name:    "mysvc",
			initsys: &mockBasicManager{},
			fs:      &mockFS{},
		}
		err := svc.SetEnvironment(ctx, env)
		require.ErrorIs(t, err, errEnvManagerNotSupported)
	})

	t.Run("fs not available", func(t *testing.T) {
		svc := &Service{
			runner:  rigtest.NewMockRunner(),
			name:    "mysvc",
			initsys: &mockEnvManager{envPath: "/etc/conf.d/mysvc"},
		}
		err := svc.SetEnvironment(ctx, env)
		require.ErrorIs(t, err, errServiceFSNotAvailable)
	})
}

func TestServiceDaemonReload(t *testing.T) {
	ctx := context.Background()

	t.Run("supported", func(t *testing.T) {
		mgr := &mockReloadEnvManager{}
		svc := &Service{
			runner:  rigtest.NewMockRunner(),
			name:    "mysvc",
			initsys: mgr,
		}
		require.NoError(t, svc.DaemonReload(ctx))
		require.True(t, mgr.reloaded)
	})

	t.Run("not supported", func(t *testing.T) {
		svc := &Service{
			runner:  rigtest.NewMockRunner(),
			name:    "mysvc",
			initsys: &mockBasicManager{},
		}
		err := svc.DaemonReload(ctx)
		require.ErrorIs(t, err, errDaemonReloadNotSupported)
	})
}

// mockLifecycleManager tracks Start/Stop calls and maintains isRunning state.
type mockLifecycleManager struct {
	isRunning   bool
	neverReady  bool // when true, ServiceIsRunning always returns false
	startCalled int
	stopCalled  int
}

func (m *mockLifecycleManager) StartService(_ context.Context, _ cmd.ContextRunner, _ string) error {
	m.startCalled++
	if !m.neverReady {
		m.isRunning = true
	}
	return nil
}

func (m *mockLifecycleManager) StopService(_ context.Context, _ cmd.ContextRunner, _ string) error {
	m.stopCalled++
	m.isRunning = false
	return nil
}

func (m *mockLifecycleManager) ServiceIsRunning(_ context.Context, _ cmd.ContextRunner, _ string) bool {
	if m.neverReady {
		return false
	}
	return m.isRunning
}

func (m *mockLifecycleManager) ServiceScriptPath(_ context.Context, _ cmd.ContextRunner, _ string) (string, error) {
	return "", nil
}

func (m *mockLifecycleManager) EnableService(_ context.Context, _ cmd.ContextRunner, _ string) error {
	return nil
}

func (m *mockLifecycleManager) DisableService(_ context.Context, _ cmd.ContextRunner, _ string) error {
	return nil
}

// mockNativeRestarter extends mockLifecycleManager with RestartService.
type mockNativeRestarter struct {
	mockLifecycleManager
	restartCalled int
	restartErr    error
}

func (m *mockNativeRestarter) RestartService(_ context.Context, _ cmd.ContextRunner, _ string) error {
	m.restartCalled++
	if m.restartErr != nil {
		return m.restartErr
	}
	if !m.neverReady {
		m.isRunning = true
	}
	return nil
}

func TestServiceStart(t *testing.T) {
	ctx := context.Background()

	t.Run("starts and reaches running state", func(t *testing.T) {
		mgr := &mockLifecycleManager{}
		svc := &Service{runner: rigtest.NewMockRunner(), name: "svc", initsys: mgr}
		require.NoError(t, svc.Start(ctx))
		require.Equal(t, 1, mgr.startCalled)
		require.True(t, mgr.isRunning)
	})

	t.Run("pre-cancelled context returns error", func(t *testing.T) {
		mgr := &mockLifecycleManager{neverReady: true}
		svc := &Service{runner: rigtest.NewMockRunner(), name: "svc", initsys: mgr}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := svc.Start(ctx)
		require.ErrorIs(t, err, context.Canceled)
	})
}

func TestServiceStop(t *testing.T) {
	ctx := context.Background()

	t.Run("stops and reaches stopped state", func(t *testing.T) {
		mgr := &mockLifecycleManager{isRunning: true}
		svc := &Service{runner: rigtest.NewMockRunner(), name: "svc", initsys: mgr}
		require.NoError(t, svc.Stop(ctx))
		require.Equal(t, 1, mgr.stopCalled)
		require.False(t, mgr.isRunning)
	})
}

func TestServiceRestart(t *testing.T) {
	ctx := context.Background()

	t.Run("uses native restart when available", func(t *testing.T) {
		mgr := &mockNativeRestarter{}
		svc := &Service{runner: rigtest.NewMockRunner(), name: "svc", initsys: mgr}
		require.NoError(t, svc.Restart(ctx))
		require.Equal(t, 1, mgr.restartCalled)
		require.Equal(t, 0, mgr.startCalled, "Start must not be called after native restart")
		require.Equal(t, 0, mgr.stopCalled, "Stop must not be called after native restart")
	})

	t.Run("falls back to stop+start without native restart", func(t *testing.T) {
		mgr := &mockLifecycleManager{isRunning: true}
		svc := &Service{runner: rigtest.NewMockRunner(), name: "svc", initsys: mgr}
		require.NoError(t, svc.Restart(ctx))
		require.Equal(t, 1, mgr.stopCalled)
		require.Equal(t, 1, mgr.startCalled)
	})

	t.Run("native restart error does not fall through to stop+start", func(t *testing.T) {
		restartErr := errors.New("restart failed")
		mgr := &mockNativeRestarter{restartErr: restartErr}
		svc := &Service{runner: rigtest.NewMockRunner(), name: "svc", initsys: mgr}
		err := svc.Restart(ctx)
		require.Error(t, err)
		require.ErrorIs(t, err, restartErr)
		require.Equal(t, 0, mgr.startCalled, "Start must not be called after failed native restart")
		require.Equal(t, 0, mgr.stopCalled, "Stop must not be called after failed native restart")
	})
}

// mockLogStreamer implements ServiceManager + ServiceManagerLogStreamer.
type mockLogStreamer struct {
	mockBasicManager
	output string
	err    error
}

func (m *mockLogStreamer) StreamServiceLogs(_ context.Context, _ cmd.ContextRunner, _ string, w io.Writer) error {
	if m.err != nil {
		return m.err
	}
	_, _ = w.Write([]byte(m.output))
	return nil
}

func TestServiceStreamLogs(t *testing.T) {
	ctx := context.Background()

	t.Run("delegates to streamer and writes output", func(t *testing.T) {
		mgr := &mockLogStreamer{output: "log line\n"}
		svc := &Service{runner: rigtest.NewMockRunner(), name: "svc", initsys: mgr}
		var buf bytes.Buffer
		require.NoError(t, svc.StreamLogs(ctx, &buf))
		require.Equal(t, "log line\n", buf.String())
	})

	t.Run("propagates streamer error", func(t *testing.T) {
		streamErr := errors.New("stream error")
		mgr := &mockLogStreamer{err: streamErr}
		svc := &Service{runner: rigtest.NewMockRunner(), name: "svc", initsys: mgr}
		err := svc.StreamLogs(ctx, io.Discard)
		require.ErrorIs(t, err, streamErr)
	})

	t.Run("context cancellation returns nil", func(t *testing.T) {
		cancelCtx, cancel := context.WithCancel(ctx)
		cancel()
		mgr := &mockLogStreamer{err: cancelCtx.Err()}
		svc := &Service{runner: rigtest.NewMockRunner(), name: "svc", initsys: mgr}
		require.NoError(t, svc.StreamLogs(cancelCtx, io.Discard), "context cancellation should not return an error")
	})

	t.Run("not supported", func(t *testing.T) {
		svc := &Service{runner: rigtest.NewMockRunner(), name: "svc", initsys: &mockBasicManager{}}
		err := svc.StreamLogs(ctx, io.Discard)
		require.ErrorIs(t, err, errLogStreamerNotSupported)
	})
}

// Ensure initsystem.ServiceEnvironmentManager is satisfied by types implementing it.
var _ initsystem.ServiceEnvironmentManager = (*mockEnvManager)(nil)
var _ initsystem.ServiceManagerReloader = (*mockReloadEnvManager)(nil)
var _ initsystem.ServiceManager = (*mockBasicManager)(nil)
var _ initsystem.ServiceManager = (*mockLifecycleManager)(nil)
var _ initsystem.ServiceManagerRestarter = (*mockNativeRestarter)(nil)
var _ initsystem.ServiceManagerLogStreamer = (*mockLogStreamer)(nil)
