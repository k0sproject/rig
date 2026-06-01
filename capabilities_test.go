package rig_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/k0sproject/rig/v2"
	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/initsystem"
	"github.com/k0sproject/rig/v2/os"
	"github.com/k0sproject/rig/v2/packagemanager"
	"github.com/k0sproject/rig/v2/remotefs"
	"github.com/k0sproject/rig/v2/rigtest"
	"github.com/stretchr/testify/require"
)

func TestClientCapabilities(t *testing.T) {
	detectedOS := &os.Release{ID: "alpine", Name: "Alpine Linux", Version: "3.18"}
	mockErr := errors.New("mock error")

	t.Run("all capabilities detected", func(t *testing.T) {
		conn := rigtest.NewMockConnection()
		client, err := rig.NewClient(
			rig.WithConnection(conn),
			rig.WithOSReleaseProvider(func(_ cmd.SimpleRunner) (*os.Release, error) {
				return detectedOS, nil
			}),
			rig.WithRemoteFSProvider(func(_ cmd.Runner) (remotefs.FS, error) {
				return remotefs.NewPosixFS(rigtest.NewMockRunner()), nil
			}),
			rig.WithPackageManagerProvider(func(_ cmd.ContextRunner) (packagemanager.PackageManager, error) {
				return packagemanager.NewApt(rigtest.NewMockRunner()), nil
			}),
			rig.WithInitSystemProvider(func(_ cmd.ContextRunner) (initsystem.ServiceManager, error) {
				return initsystem.Systemd{}, nil
			}),
			rig.WithSudoProvider(func(_ cmd.Runner) (cmd.Runner, error) {
				return rigtest.NewMockRunner(), nil
			}),
		)
		require.NoError(t, err)
		require.NoError(t, client.Connect(context.Background()))

		caps := client.Capabilities()

		require.Equal(t, "mock", caps.Protocol)
		require.Equal(t, detectedOS, caps.OS)
		require.NoError(t, caps.OSErr)
		require.True(t, caps.RemoteFS)
		require.NoError(t, caps.RemoteFSErr)
		require.Equal(t, "apt", caps.PackageManager)
		require.NoError(t, caps.PackageManagerErr)
		require.Equal(t, "systemd", caps.InitSystem)
		require.NoError(t, caps.InitSystemErr)
		require.True(t, caps.Sudo)
		require.NoError(t, caps.SudoErr)
		require.False(t, caps.InteractiveExec)
	})

	t.Run("all capabilities unavailable", func(t *testing.T) {
		conn := rigtest.NewMockConnection()
		client, err := rig.NewClient(
			rig.WithConnection(conn),
			rig.WithOSReleaseProvider(func(_ cmd.SimpleRunner) (*os.Release, error) {
				return nil, mockErr
			}),
			rig.WithRemoteFSProvider(func(_ cmd.Runner) (remotefs.FS, error) {
				return nil, mockErr
			}),
			rig.WithPackageManagerProvider(func(_ cmd.ContextRunner) (packagemanager.PackageManager, error) {
				return nil, mockErr
			}),
			rig.WithInitSystemProvider(func(_ cmd.ContextRunner) (initsystem.ServiceManager, error) {
				return nil, mockErr
			}),
			rig.WithSudoProvider(func(_ cmd.Runner) (cmd.Runner, error) {
				return nil, mockErr
			}),
		)
		require.NoError(t, err)
		require.NoError(t, client.Connect(context.Background()))

		caps := client.Capabilities()

		require.Nil(t, caps.OS)
		require.ErrorIs(t, caps.OSErr, mockErr)
		require.False(t, caps.RemoteFS)
		require.ErrorIs(t, caps.RemoteFSErr, mockErr)
		require.Empty(t, caps.PackageManager)
		require.ErrorIs(t, caps.PackageManagerErr, mockErr)
		require.Empty(t, caps.InitSystem)
		require.ErrorIs(t, caps.InitSystemErr, mockErr)
		require.False(t, caps.Sudo)
		require.ErrorIs(t, caps.SudoErr, mockErr)
		require.False(t, caps.InteractiveExec)
	})

	t.Run("interactive exec detected", func(t *testing.T) {
		conn := &interactiveConn{MockConnection: rigtest.NewMockConnection()}
		client, err := rig.NewClient(rig.WithConnection(conn))
		require.NoError(t, err)
		require.NoError(t, client.Connect(context.Background()))

		caps := client.Capabilities()

		require.True(t, caps.InteractiveExec)
	})

	t.Run("String output contains expected fields", func(t *testing.T) {
		conn := rigtest.NewMockConnection()
		client, err := rig.NewClient(
			rig.WithConnection(conn),
			rig.WithOSReleaseProvider(func(_ cmd.SimpleRunner) (*os.Release, error) {
				return detectedOS, nil
			}),
			rig.WithPackageManagerProvider(func(_ cmd.ContextRunner) (packagemanager.PackageManager, error) {
				return packagemanager.NewApt(rigtest.NewMockRunner()), nil
			}),
			rig.WithInitSystemProvider(func(_ cmd.ContextRunner) (initsystem.ServiceManager, error) {
				return initsystem.OpenRC{}, nil
			}),
		)
		require.NoError(t, err)
		require.NoError(t, client.Connect(context.Background()))

		s := client.Capabilities().String()

		require.Contains(t, s, "protocol=mock")
		require.Contains(t, s, "package-manager=apt")
		require.Contains(t, s, "init-system=openrc")
	})

	t.Run("caching: provider called once on double Capabilities call", func(t *testing.T) {
		calls := 0
		conn := rigtest.NewMockConnection()
		client, err := rig.NewClient(
			rig.WithConnection(conn),
			rig.WithOSReleaseProvider(func(_ cmd.SimpleRunner) (*os.Release, error) {
				calls++
				return detectedOS, nil
			}),
		)
		require.NoError(t, err)
		require.NoError(t, client.Connect(context.Background()))

		client.Capabilities()
		client.Capabilities()

		require.Equal(t, 1, calls, "OS provider should only be called once across multiple Capabilities calls")
	})
}

func TestInitSystemString(t *testing.T) {
	cases := []struct {
		want string
		is   fmt.Stringer
	}{
		{"systemd", initsystem.Systemd{}},
		{"openrc", initsystem.OpenRC{}},
		{"launchd", initsystem.Launchd{}},
		{"winscm", initsystem.WinSCM{}},
		{"upstart", initsystem.Upstart{}},
		{"sysvinit", initsystem.SysVinit{}},
		{"runit", initsystem.Runit{}},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			require.Equal(t, tc.want, tc.is.String())
		})
	}
}

func TestPackageManagerString(t *testing.T) {
	runner := rigtest.NewMockRunner()
	cases := []struct {
		want string
		pm   packagemanager.PackageManager
	}{
		{"apt", packagemanager.NewApt(runner)},
		{"yum", packagemanager.NewYum(runner)},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			s, ok := tc.pm.(fmt.Stringer)
			require.True(t, ok, "package manager %T should implement fmt.Stringer", tc.pm)
			require.Equal(t, tc.want, s.String())
		})
	}
}
