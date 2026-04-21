package rig

import (
	"context"
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

// Ensure initsystem.ServiceEnvironmentManager is satisfied by types implementing it.
var _ initsystem.ServiceEnvironmentManager = (*mockEnvManager)(nil)
var _ initsystem.ServiceManagerReloader = (*mockReloadEnvManager)(nil)
var _ initsystem.ServiceManager = (*mockBasicManager)(nil)
