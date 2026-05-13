package remotefs_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/k0sproject/rig/v2/remotefs"
	"github.com/k0sproject/rig/v2/rigtest"
	"github.com/stretchr/testify/require"
)

func TestWinFSRebootSequence(t *testing.T) {
	runner := rigtest.NewMockRunner()
	runner.Windows = true
	runner.AddCommandSuccess(rigtest.Contains("schtasks /create"))
	runner.AddCommandSuccess(rigtest.Contains("schtasks /run"))
	runner.AddCommandSuccess(rigtest.Contains("schtasks /delete"))

	fs := remotefs.NewWindowsFS(runner)
	require.NoError(t, fs.Reboot(context.Background()))
	require.NoError(t, runner.Received(rigtest.Contains(`/tr "shutdown /r /f /t 5"`)))
	require.NoError(t, runner.Received(rigtest.Contains(`/sc once /st 23:59 /z /f /ru SYSTEM`)))
	require.NoError(t, runner.Received(rigtest.Contains("schtasks /run")))
	require.NoError(t, runner.Received(rigtest.Contains("schtasks /delete")))
}

func TestWinFSRebootCreateFails(t *testing.T) {
	runner := rigtest.NewMockRunner()
	runner.Windows = true
	runner.AddCommandFailure(rigtest.Contains("schtasks /create"), errors.New("access denied"))

	fs := remotefs.NewWindowsFS(runner)
	err := fs.Reboot(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "create reboot task")
	require.Error(t, runner.Received(rigtest.Contains("schtasks /run")))
}

func TestWinFSRebootRunConnectionError(t *testing.T) {
	runner := rigtest.NewMockRunner()
	runner.Windows = true
	runner.AddCommandSuccess(rigtest.Contains("schtasks /create"))
	runner.AddCommandFailure(rigtest.Contains("schtasks /run"), io.EOF)
	runner.AddCommandSuccess(rigtest.Contains("schtasks /delete"))

	fs := remotefs.NewWindowsFS(runner)
	require.NoError(t, fs.Reboot(context.Background()), "transport-level run error should be treated as success")
	require.NoError(t, runner.Received(rigtest.Contains("schtasks /delete")))
}

func TestWinFSRebootRunLogicalError(t *testing.T) {
	runner := rigtest.NewMockRunner()
	runner.Windows = true
	runner.AddCommandSuccess(rigtest.Contains("schtasks /create"))
	runner.AddCommandFailure(rigtest.Contains("schtasks /run"), errors.New("exit code 1"))
	runner.AddCommandSuccess(rigtest.Contains("schtasks /delete"))

	fs := remotefs.NewWindowsFS(runner)
	err := fs.Reboot(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "run reboot task")
	require.NoError(t, runner.Received(rigtest.Contains("schtasks /delete")))
}

func TestWinFSRebootUniqueTaskNames(t *testing.T) {
	runner := rigtest.NewMockRunner()
	runner.Windows = true
	runner.AddCommandSuccess(rigtest.Contains("schtasks"))

	fs := remotefs.NewWindowsFS(runner)
	require.NoError(t, fs.Reboot(context.Background()))
	require.NoError(t, fs.Reboot(context.Background()))

	var names []string
	for _, c := range runner.MockConnection.Commands() {
		if !strings.Contains(c, "schtasks /create") {
			continue
		}
		const marker = `/tn "`
		i := strings.Index(c, marker)
		if i < 0 {
			continue
		}
		rest := c[i+len(marker):]
		j := strings.Index(rest, `"`)
		if j < 0 {
			continue
		}
		names = append(names, rest[:j])
	}
	require.Len(t, names, 2)
	require.NotEqual(t, names[0], names[1], "task names should be unique per call")
}

func TestPosixFSRebootSuccess(t *testing.T) {
	runner := rigtest.NewMockRunner()
	runner.AddCommandSuccess(rigtest.Equal("reboot"))

	fs := remotefs.NewPosixFS(runner)
	require.NoError(t, fs.Reboot(context.Background()))
	require.NoError(t, runner.Received(rigtest.Equal("reboot")))
}

func TestPosixFSRebootTransportClosedTreatedAsSuccess(t *testing.T) {
	runner := rigtest.NewMockRunner()
	runner.AddCommandFailure(rigtest.Equal("reboot"), io.EOF)

	fs := remotefs.NewPosixFS(runner)
	require.NoError(t, fs.Reboot(context.Background()))
}

func TestPosixFSRebootFallbackToShutdown(t *testing.T) {
	runner := rigtest.NewMockRunner()
	runner.AddCommandFailure(rigtest.Equal("reboot"), errors.New("command not found"))
	runner.AddCommandSuccess(rigtest.Equal("shutdown -r now"))

	fs := remotefs.NewPosixFS(runner)
	require.NoError(t, fs.Reboot(context.Background()))
	require.NoError(t, runner.Received(rigtest.Equal("shutdown -r now")))
}

func TestPosixFSRebootBothFail(t *testing.T) {
	runner := rigtest.NewMockRunner()
	runner.AddCommandFailure(rigtest.Equal("reboot"), errors.New("not found"))
	runner.AddCommandFailure(rigtest.Equal("shutdown -r now"), errors.New("also not found"))

	fs := remotefs.NewPosixFS(runner)
	err := fs.Reboot(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
	require.Contains(t, err.Error(), "shutdown -r now")
}
