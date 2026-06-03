package initsystem_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/k0sproject/rig/v2/initsystem"
	"github.com/k0sproject/rig/v2/rigtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errExec is a sentinel used by service-op error tests.
var errExec = errors.New("exec error")

// TestSystemdServiceOps covers the core operations of the Systemd init system.
func TestSystemdServiceOps(t *testing.T) {
	ctx := context.Background()
	svc := initsystem.Systemd{}

	t.Run("StartService sends correct command", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("systemctl"))
		require.NoError(t, svc.StartService(ctx, mr, "k0s"))
		require.NoError(t, mr.Received(rigtest.Equal("systemctl start k0s")))
	})

	t.Run("StartService propagates error", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandFailure(rigtest.Contains("systemctl"), errExec)
		require.ErrorIs(t, svc.StartService(ctx, mr, "k0s"), errExec)
	})

	t.Run("StopService sends correct command", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("systemctl"))
		require.NoError(t, svc.StopService(ctx, mr, "k0s"))
		require.NoError(t, mr.Received(rigtest.Equal("systemctl stop k0s")))
	})

	t.Run("StopService propagates error", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandFailure(rigtest.Contains("systemctl"), errExec)
		require.ErrorIs(t, svc.StopService(ctx, mr, "k0s"), errExec)
	})

	t.Run("RestartService sends correct command", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("systemctl"))
		require.NoError(t, svc.RestartService(ctx, mr, "k0s"))
		require.NoError(t, mr.Received(rigtest.Equal("systemctl restart k0s")))
	})

	t.Run("EnableService sends correct command", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("systemctl"))
		require.NoError(t, svc.EnableService(ctx, mr, "k0s"))
		require.NoError(t, mr.Received(rigtest.Equal("systemctl enable k0s")))
	})

	t.Run("DisableService sends correct command", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("systemctl"))
		require.NoError(t, svc.DisableService(ctx, mr, "k0s"))
		require.NoError(t, mr.Received(rigtest.Equal("systemctl disable k0s")))
	})

	t.Run("DaemonReload sends correct command", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("systemctl"))
		require.NoError(t, svc.DaemonReload(ctx, mr))
		require.NoError(t, mr.Received(rigtest.Equal("systemctl daemon-reload")))
	})

	t.Run("DaemonReload propagates error", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandFailure(rigtest.Contains("systemctl"), errExec)
		require.ErrorIs(t, svc.DaemonReload(ctx, mr), errExec)
	})

	t.Run("ServiceIsRunning returns true when command succeeds", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("systemctl status"))
		assert.True(t, svc.ServiceIsRunning(ctx, mr, "k0s"))
	})

	t.Run("ServiceIsRunning returns false when command fails", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.ErrDefault = errExec
		assert.False(t, svc.ServiceIsRunning(ctx, mr, "k0s"))
	})

	t.Run("ServiceScriptPath returns FragmentPath output", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandOutput(rigtest.Contains("FragmentPath"), "/usr/lib/systemd/system/k0s.service\n")
		path, err := svc.ServiceScriptPath(ctx, mr, "k0s")
		require.NoError(t, err)
		assert.Equal(t, "/usr/lib/systemd/system/k0s.service", path)
	})

	t.Run("ServiceScriptPath propagates error", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.ErrDefault = errExec
		_, err := svc.ServiceScriptPath(ctx, mr, "k0s")
		require.ErrorIs(t, err, errExec)
	})

	t.Run("ServiceLogs returns split output", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandOutput(rigtest.Contains("journalctl"), "line1\nline2\nline3")
		lines, err := svc.ServiceLogs(ctx, mr, "k0s", 3)
		require.NoError(t, err)
		assert.Equal(t, []string{"line1", "line2", "line3"}, lines)
	})
}

// TestOpenRCServiceOps covers the core operations of the OpenRC init system.
func TestOpenRCServiceOps(t *testing.T) {
	ctx := context.Background()
	svc := initsystem.OpenRC{}

	t.Run("StartService sends correct command", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("rc-service"))
		require.NoError(t, svc.StartService(ctx, mr, "k0s"))
		require.NoError(t, mr.Received(rigtest.Equal("rc-service k0s start")))
	})

	t.Run("StartService propagates error", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandFailure(rigtest.Contains("rc-service"), errExec)
		require.ErrorIs(t, svc.StartService(ctx, mr, "k0s"), errExec)
	})

	t.Run("StopService sends correct command", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("rc-service"))
		require.NoError(t, svc.StopService(ctx, mr, "k0s"))
		require.NoError(t, mr.Received(rigtest.Equal("rc-service k0s stop")))
	})

	t.Run("RestartService sends correct command", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("rc-service"))
		require.NoError(t, svc.RestartService(ctx, mr, "k0s"))
		require.NoError(t, mr.Received(rigtest.Equal("rc-service k0s restart")))
	})

	t.Run("EnableService sends rc-update add", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("rc-update"))
		require.NoError(t, svc.EnableService(ctx, mr, "k0s"))
		require.NoError(t, mr.Received(rigtest.Equal("rc-update add k0s")))
	})

	t.Run("DisableService sends rc-update del", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("rc-update"))
		require.NoError(t, svc.DisableService(ctx, mr, "k0s"))
		require.NoError(t, mr.Received(rigtest.Equal("rc-update del k0s")))
	})

	t.Run("ServiceIsRunning returns true when command succeeds", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("rc-service"))
		assert.True(t, svc.ServiceIsRunning(ctx, mr, "k0s"))
	})

	t.Run("ServiceIsRunning returns false when command fails", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.ErrDefault = errExec
		assert.False(t, svc.ServiceIsRunning(ctx, mr, "k0s"))
	})

	t.Run("ServiceScriptPath returns output trimmed", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandOutput(rigtest.Contains("rc-service"), "/etc/init.d/k0s\n")
		path, err := svc.ServiceScriptPath(ctx, mr, "k0s")
		require.NoError(t, err)
		assert.Equal(t, "/etc/init.d/k0s", path)
	})
}

// TestSysVinitServiceOps covers SysVinit including both enable code paths.
func TestSysVinitServiceOps(t *testing.T) {
	ctx := context.Background()
	svc := initsystem.SysVinit{}

	t.Run("StartService sends correct command", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.HasPrefix("/etc/init.d/"))
		require.NoError(t, svc.StartService(ctx, mr, "k0s"))
		require.NoError(t, mr.Received(rigtest.Equal("/etc/init.d/k0s start")))
	})

	t.Run("StopService sends correct command", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.HasPrefix("/etc/init.d/"))
		require.NoError(t, svc.StopService(ctx, mr, "k0s"))
		require.NoError(t, mr.Received(rigtest.Equal("/etc/init.d/k0s stop")))
	})

	t.Run("RestartService sends correct command", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.HasPrefix("/etc/init.d/"))
		require.NoError(t, svc.RestartService(ctx, mr, "k0s"))
		require.NoError(t, mr.Received(rigtest.Equal("/etc/init.d/k0s restart")))
	})

	t.Run("ServiceIsRunning returns true when script status succeeds", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.HasPrefix("/etc/init.d/"))
		assert.True(t, svc.ServiceIsRunning(ctx, mr, "k0s"))
	})

	t.Run("ServiceIsRunning returns false when script status fails", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.ErrDefault = errExec
		assert.False(t, svc.ServiceIsRunning(ctx, mr, "k0s"))
	})

	t.Run("ServiceScriptPath returns init.d path without runner call", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		path, err := svc.ServiceScriptPath(ctx, mr, "k0s")
		require.NoError(t, err)
		assert.Equal(t, "/etc/init.d/k0s", path)
		assert.Equal(t, 0, mr.Len())
	})

	t.Run("EnableService uses chkconfig when available", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("chkconfig"))
		require.NoError(t, svc.EnableService(ctx, mr, "k0s"))
		require.NoError(t, mr.Received(rigtest.Equal("chkconfig --add k0s")))
		require.NoError(t, mr.NotReceived(rigtest.Contains("ln -s")))
	})

	t.Run("EnableService creates symlinks when chkconfig absent", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandFailure(rigtest.Contains("chkconfig"), errExec)
		mr.AddCommandSuccess(rigtest.Contains("ln -s"))
		require.NoError(t, svc.EnableService(ctx, mr, "k0s"))
		// Four runlevels (2-5) each get a symlink.
		for runlevel := 2; runlevel <= 5; runlevel++ {
			want := fmt.Sprintf("ln -s /etc/init.d/k0s /etc/rc%d.d/S99k0s", runlevel)
			require.NoError(t, mr.Received(rigtest.Equal(want)))
		}
	})

	t.Run("DisableService uses chkconfig when available", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("chkconfig"))
		require.NoError(t, svc.DisableService(ctx, mr, "k0s"))
		require.NoError(t, mr.Received(rigtest.Equal("chkconfig --del k0s")))
		require.NoError(t, mr.NotReceived(rigtest.Contains("rm -f")))
	})

	t.Run("DisableService removes symlinks when chkconfig absent", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandFailure(rigtest.Contains("chkconfig"), errExec)
		mr.AddCommandSuccess(rigtest.Contains("rm -f"))
		require.NoError(t, svc.DisableService(ctx, mr, "k0s"))
		for runlevel := 2; runlevel <= 5; runlevel++ {
			want := fmt.Sprintf("rm -f /etc/rc%d.d/S99k0s", runlevel)
			require.NoError(t, mr.Received(rigtest.Equal(want)))
		}
	})
}

// TestUpstartServiceOps covers the Upstart init system.
func TestUpstartServiceOps(t *testing.T) {
	ctx := context.Background()
	svc := initsystem.Upstart{}

	t.Run("StartService sends correct command", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("initctl"))
		require.NoError(t, svc.StartService(ctx, mr, "k0s"))
		require.NoError(t, mr.Received(rigtest.Equal("initctl start k0s")))
	})

	t.Run("StopService sends correct command", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("initctl"))
		require.NoError(t, svc.StopService(ctx, mr, "k0s"))
		require.NoError(t, mr.Received(rigtest.Equal("initctl stop k0s")))
	})

	t.Run("ServiceIsRunning returns true on success", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("initctl"))
		assert.True(t, svc.ServiceIsRunning(ctx, mr, "k0s"))
	})

	t.Run("ServiceIsRunning returns false on failure", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.ErrDefault = errExec
		assert.False(t, svc.ServiceIsRunning(ctx, mr, "k0s"))
	})

	t.Run("ServiceScriptPath returns correct conf path", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		path, err := svc.ServiceScriptPath(ctx, mr, "k0s")
		require.NoError(t, err)
		assert.Equal(t, "/etc/init/k0s.conf", path)
		assert.Equal(t, 0, mr.Len())
	})

	t.Run("EnableService removes override file", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("rm -f"))
		require.NoError(t, svc.EnableService(ctx, mr, "k0s"))
		require.NoError(t, mr.Received(rigtest.Contains("/etc/init/k0s.override")))
	})

	t.Run("DisableService creates override file", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("tee"))
		require.NoError(t, svc.DisableService(ctx, mr, "k0s"))
		require.NoError(t, mr.Received(rigtest.Contains("/etc/init/k0s.override")))
	})

	t.Run("ServiceLogs returns split lines", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandOutput(rigtest.Contains("tail"), "a\nb")
		lines, err := svc.ServiceLogs(ctx, mr, "k0s", 2)
		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b"}, lines)
	})
}

// TestRunitServiceOps covers the Runit init system.
func TestRunitServiceOps(t *testing.T) {
	ctx := context.Background()
	svc := initsystem.Runit{}

	t.Run("StartService sends correct command", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("sv"))
		require.NoError(t, svc.StartService(ctx, mr, "k0s"))
		require.NoError(t, mr.Received(rigtest.Equal("sv start k0s")))
	})

	t.Run("StopService sends correct command", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("sv"))
		require.NoError(t, svc.StopService(ctx, mr, "k0s"))
		require.NoError(t, mr.Received(rigtest.Equal("sv stop k0s")))
	})

	t.Run("RestartService sends correct command", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("sv"))
		require.NoError(t, svc.RestartService(ctx, mr, "k0s"))
		require.NoError(t, mr.Received(rigtest.Equal("sv restart k0s")))
	})

	t.Run("ServiceIsRunning returns true on success", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("sv"))
		assert.True(t, svc.ServiceIsRunning(ctx, mr, "k0s"))
	})

	t.Run("ServiceIsRunning returns false on failure", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.ErrDefault = errExec
		assert.False(t, svc.ServiceIsRunning(ctx, mr, "k0s"))
	})

	t.Run("ServiceScriptPath returns correct path", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		path, err := svc.ServiceScriptPath(ctx, mr, "k0s")
		require.NoError(t, err)
		assert.Equal(t, "/etc/service/k0s", path)
		assert.Equal(t, 0, mr.Len())
	})

	t.Run("EnableService creates symlink", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("ln -s"))
		require.NoError(t, svc.EnableService(ctx, mr, "k0s"))
		require.NoError(t, mr.Received(rigtest.Contains("/etc/sv/k0s/run")))
	})

	t.Run("DisableService removes symlink", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("rm -f"))
		require.NoError(t, svc.DisableService(ctx, mr, "k0s"))
		require.NoError(t, mr.Received(rigtest.Contains("/etc/service/k0s")))
	})

	t.Run("ServiceLogs returns split lines", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandOutput(rigtest.Contains("tail"), "x\ny")
		lines, err := svc.ServiceLogs(ctx, mr, "k0s", 2)
		require.NoError(t, err)
		assert.Equal(t, []string{"x", "y"}, lines)
	})
}

// TestLaunchdServiceOps covers the Launchd init system.
func TestLaunchdServiceOps(t *testing.T) {
	ctx := context.Background()
	svc := initsystem.Launchd{}

	t.Run("StartService sends kickstart command", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("launchctl"))
		require.NoError(t, svc.StartService(ctx, mr, "com.example.k0s"))
		require.NoError(t, mr.Received(rigtest.Equal("launchctl kickstart com.example.k0s")))
	})

	t.Run("StopService sends kill command", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("launchctl"))
		require.NoError(t, svc.StopService(ctx, mr, "com.example.k0s"))
		require.NoError(t, mr.Received(rigtest.Equal("launchctl kill com.example.k0s")))
	})

	t.Run("ServiceIsRunning returns true when listed", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("launchctl"))
		assert.True(t, svc.ServiceIsRunning(ctx, mr, "com.example.k0s"))
	})

	t.Run("ServiceIsRunning returns false when not listed", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.ErrDefault = errExec
		assert.False(t, svc.ServiceIsRunning(ctx, mr, "com.example.k0s"))
	})

	t.Run("ServiceScriptPath returns plist path", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		path, err := svc.ServiceScriptPath(ctx, mr, "com.example.k0s")
		require.NoError(t, err)
		assert.Equal(t, "/Library/LaunchDaemons/com.example.k0s.plist", path)
		assert.Equal(t, 0, mr.Len())
	})

	t.Run("EnableService sends launchctl enable", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("launchctl enable"))
		require.NoError(t, svc.EnableService(ctx, mr, "com.example.k0s"))
	})

	t.Run("DisableService sends launchctl disable", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("launchctl disable"))
		require.NoError(t, svc.DisableService(ctx, mr, "com.example.k0s"))
	})
}

// TestRegistryDetection verifies the Register* functions add detectors that return the right
// manager when the probe command succeeds and skip when it fails or IsWindows is true.
func TestRegistryDetection(t *testing.T) {
	t.Run("Systemd detected when systemd socket present", func(t *testing.T) {
		reg := initsystem.NewRegistry()
		initsystem.RegisterSystemd(reg)

		mr := rigtest.NewMockRunner()
		mr.ErrDefault = errExec
		mr.AddCommandSuccess(rigtest.Contains("stat /run/systemd/system"))

		mgr, err := reg.Get(mr)
		require.NoError(t, err)
		assert.IsType(t, initsystem.Systemd{}, mgr)
		require.NoError(t, mr.Received(rigtest.Contains("stat /run/systemd/system")))
	})

	t.Run("Systemd not detected when probe fails", func(t *testing.T) {
		reg := initsystem.NewRegistry()
		initsystem.RegisterSystemd(reg)

		mr := rigtest.NewMockRunner()
		mr.ErrDefault = errExec

		_, err := reg.Get(mr)
		require.ErrorIs(t, err, initsystem.ErrNoInitSystem)
	})

	t.Run("Systemd skipped on Windows", func(t *testing.T) {
		reg := initsystem.NewRegistry()
		initsystem.RegisterSystemd(reg)

		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandSuccess(rigtest.Contains("stat /run/systemd/system"))

		_, err := reg.Get(mr)
		require.ErrorIs(t, err, initsystem.ErrNoInitSystem)
		// Probe command must NOT have been sent.
		require.NoError(t, mr.NotReceived(rigtest.Contains("stat /run/systemd/system")))
	})

	t.Run("SysVinit detected when /etc/init.d exists", func(t *testing.T) {
		reg := initsystem.NewRegistry()
		initsystem.RegisterSysVinit(reg)

		mr := rigtest.NewMockRunner()
		mr.ErrDefault = errExec
		mr.AddCommandSuccess(rigtest.Contains("/etc/init.d"))

		mgr, err := reg.Get(mr)
		require.NoError(t, err)
		assert.IsType(t, initsystem.SysVinit{}, mgr)
		require.NoError(t, mr.Received(rigtest.Contains("/etc/init.d")))
	})

	t.Run("SysVinit not detected on Windows", func(t *testing.T) {
		reg := initsystem.NewRegistry()
		initsystem.RegisterSysVinit(reg)

		mr := rigtest.NewMockRunner()
		mr.Windows = true

		_, err := reg.Get(mr)
		require.ErrorIs(t, err, initsystem.ErrNoInitSystem)
	})

	t.Run("Runit detected when both runit and sv present", func(t *testing.T) {
		reg := initsystem.NewRegistry()
		initsystem.RegisterRunit(reg)

		mr := rigtest.NewMockRunner()
		mr.ErrDefault = errExec
		mr.AddCommandSuccess(rigtest.Contains("command -v runit"))
		mr.AddCommandSuccess(rigtest.Contains("command -v sv"))

		mgr, err := reg.Get(mr)
		require.NoError(t, err)
		assert.IsType(t, initsystem.Runit{}, mgr)
		require.NoError(t, mr.Received(rigtest.Contains("command -v runit")))
		require.NoError(t, mr.Received(rigtest.Contains("command -v sv")))
	})

	t.Run("Upstart detected when initctl present", func(t *testing.T) {
		reg := initsystem.NewRegistry()
		initsystem.RegisterUpstart(reg)

		mr := rigtest.NewMockRunner()
		mr.ErrDefault = errExec
		mr.AddCommandSuccess(rigtest.Contains("command -v initctl"))

		mgr, err := reg.Get(mr)
		require.NoError(t, err)
		assert.IsType(t, initsystem.Upstart{}, mgr)
		require.NoError(t, mr.Received(rigtest.Contains("command -v initctl")))
	})

	t.Run("Launchd detected when SystemVersion.plist present", func(t *testing.T) {
		reg := initsystem.NewRegistry()
		initsystem.RegisterLaunchd(reg)

		mr := rigtest.NewMockRunner()
		mr.ErrDefault = errExec
		mr.AddCommandSuccess(rigtest.Contains("SystemVersion.plist"))

		mgr, err := reg.Get(mr)
		require.NoError(t, err)
		assert.IsType(t, initsystem.Launchd{}, mgr)
		require.NoError(t, mr.Received(rigtest.Contains("SystemVersion.plist")))
	})

	t.Run("First registered manager wins", func(t *testing.T) {
		reg := initsystem.NewRegistry()
		initsystem.RegisterSystemd(reg)
		initsystem.RegisterSysVinit(reg)

		mr := rigtest.NewMockRunner()
		// Both probes would succeed; systemd is first.
		mr.AddCommandSuccess(rigtest.HasPrefix("stat "))
		mr.AddCommandSuccess(rigtest.HasPrefix("test "))

		mgr, err := reg.Get(mr)
		require.NoError(t, err)
		assert.IsType(t, initsystem.Systemd{}, mgr)
	})

	t.Run("OpenRC not detected on Windows", func(t *testing.T) {
		reg := initsystem.NewRegistry()
		initsystem.RegisterOpenRC(reg)

		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandSuccess(rigtest.Contains("openrc"))

		_, err := reg.Get(mr)
		require.ErrorIs(t, err, initsystem.ErrNoInitSystem)
		require.NoError(t, mr.NotReceived(rigtest.Contains("openrc")))
	})
}

// Compile-time interface checks.
var (
	_ initsystem.ServiceManager            = initsystem.Systemd{}
	_ initsystem.ServiceManager            = initsystem.OpenRC{}
	_ initsystem.ServiceManager            = initsystem.SysVinit{}
	_ initsystem.ServiceManager            = initsystem.Upstart{}
	_ initsystem.ServiceManager            = initsystem.Runit{}
	_ initsystem.ServiceManager            = initsystem.Launchd{}
	_ initsystem.ServiceManagerRestarter   = initsystem.Systemd{}
	_ initsystem.ServiceManagerRestarter   = initsystem.OpenRC{}
	_ initsystem.ServiceManagerRestarter   = initsystem.SysVinit{}
	_ initsystem.ServiceManagerRestarter   = initsystem.Upstart{}
	_ initsystem.ServiceManagerRestarter   = initsystem.Runit{}
	_ initsystem.ServiceManagerReloader    = initsystem.Systemd{}
	_ initsystem.ServiceManagerLogReader   = initsystem.Systemd{}
	_ initsystem.ServiceManagerLogReader   = initsystem.Upstart{}
	_ initsystem.ServiceManagerLogReader   = initsystem.Runit{}
	_ initsystem.ServiceManagerLogReader   = initsystem.Launchd{}
	_ initsystem.ServiceEnvironmentManager = initsystem.Systemd{}
	_ initsystem.ServiceEnvironmentManager = initsystem.OpenRC{}
)
