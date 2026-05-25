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
	runner.AddCommandSuccess(rigtest.HasPrefix("powershell.exe"))

	fs := remotefs.NewWindowsFS(runner)
	require.NoError(t, fs.Reboot(context.Background()))

	cmds := runner.MockConnection.Commands()
	require.Len(t, cmds, 3, "expected create + run + delete = 3 commands")

	create, ok := decodePSScript(cmds[0])
	require.True(t, ok, "create command should be an encoded PS command")
	require.Contains(t, create, "Register-ScheduledTask")
	require.Contains(t, create, "shutdown.exe")
	require.Contains(t, create, "SYSTEM")
	require.Contains(t, create, "/r /f /t 5")

	run, ok := decodePSScript(cmds[1])
	require.True(t, ok, "run command should be an encoded PS command")
	require.Contains(t, run, "Start-ScheduledTask")

	del, ok := decodePSScript(cmds[2])
	require.True(t, ok, "delete command should be an encoded PS command")
	require.Contains(t, del, "Unregister-ScheduledTask")

	// Must not fall back to the old schtasks CLI approach.
	require.NoError(t, runner.NotReceived(rigtest.Contains("schtasks")))
}

func TestWinFSRebootCreateFails(t *testing.T) {
	runner := rigtest.NewMockRunner()
	runner.Windows = true
	runner.AddCommandFailure(rigtest.HasPrefix("powershell.exe"), errors.New("access denied"))

	fs := remotefs.NewWindowsFS(runner)
	err := fs.Reboot(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "create reboot task")
	require.Equal(t, 1, runner.Len(), "run must not be called after a create failure")
}

func TestWinFSRebootRunConnectionError(t *testing.T) {
	runner := rigtest.NewMockRunner()
	runner.Windows = true
	call := 0
	runner.AddCommand(rigtest.HasPrefix("powershell.exe"), func(_ *rigtest.A) error {
		call++
		if call == 2 { // run script
			return io.EOF // transport-level error
		}
		return nil
	})

	fs := remotefs.NewWindowsFS(runner)
	require.NoError(t, fs.Reboot(context.Background()), "transport-level run error should be treated as success")
	require.Equal(t, 3, runner.Len(), "delete must still be attempted after transport error")
}

func TestWinFSRebootRunLogicalError(t *testing.T) {
	runner := rigtest.NewMockRunner()
	runner.Windows = true
	call := 0
	runner.AddCommand(rigtest.HasPrefix("powershell.exe"), func(_ *rigtest.A) error {
		call++
		if call == 2 { // run script
			return errors.New("exit code 1")
		}
		return nil
	})

	fs := remotefs.NewWindowsFS(runner)
	err := fs.Reboot(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "run reboot task")
	require.Equal(t, 3, runner.Len(), "delete must still be attempted after logical run error")
}

func TestWinFSRebootUniqueTaskNames(t *testing.T) {
	runner := rigtest.NewMockRunner()
	runner.Windows = true
	runner.AddCommandSuccess(rigtest.HasPrefix("powershell.exe"))

	fs := remotefs.NewWindowsFS(runner)
	require.NoError(t, fs.Reboot(context.Background()))
	require.NoError(t, fs.Reboot(context.Background()))

	// Extract task names from the Start-ScheduledTask (run) commands — index 1 and 4.
	cmds := runner.MockConnection.Commands()
	require.Len(t, cmds, 6, "2 reboots × 3 commands each = 6")

	extractTaskName := func(cmd string) string {
		t.Helper()
		decoded, ok := decodePSScript(cmd)
		require.True(t, ok)
		const marker = "Start-ScheduledTask -TaskName \""
		i := strings.Index(decoded, marker)
		require.GreaterOrEqual(t, i, 0, "run command must contain Start-ScheduledTask -TaskName")
		rest := decoded[i+len(marker):]
		j := strings.Index(rest, "\"")
		require.GreaterOrEqual(t, j, 0)
		return rest[:j]
	}

	name1 := extractTaskName(cmds[1]) // run command of first Reboot
	name2 := extractTaskName(cmds[4]) // run command of second Reboot
	require.NotEqual(t, name1, name2, "task names should be unique per call")
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
