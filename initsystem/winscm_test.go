package initsystem_test

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/k0sproject/rig/v2/initsystem"
	"github.com/k0sproject/rig/v2/rigtest"
	"github.com/stretchr/testify/require"
)

func newWinRunner() *rigtest.MockRunner {
	mr := rigtest.NewMockRunner()
	mr.Windows = true
	return mr
}

// decodePSCmd decodes a powershell.exe -E <base64> command back to the
// original PowerShell source using proper UTF-16LE decoding. It asserts
// that the command is in encoded form and fails the test otherwise.
func decodePSCmd(t *testing.T, cmd string) string {
	t.Helper()
	_, b64, ok := strings.Cut(cmd, " -E ")
	require.True(t, ok, "command should use -EncodedCommand (-E) form: %q", cmd)
	data, err := base64.StdEncoding.DecodeString(b64)
	require.NoError(t, err, "base64 decode failed")
	require.True(t, len(data)%2 == 0, "decoded bytes must be even (UTF-16LE)")
	words := make([]uint16, len(data)/2)
	for i := range words {
		words[i] = uint16(data[i*2]) | uint16(data[i*2+1])<<8
	}
	return string(utf16.Decode(words))
}

func TestWinSCMStartService(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mr := newWinRunner()
		mr.AddCommandSuccess(rigtest.HasPrefix("powershell.exe"))
		require.NoError(t, initsystem.WinSCM{}.StartService(context.Background(), mr, "myservice"))
		require.NoError(t, mr.NotReceived(rigtest.Contains("sc.exe")))
		script := decodePSCmd(t, mr.LastCommand())
		require.Contains(t, script, "Start-Service")
		require.Contains(t, script, "-ErrorAction Stop")
		require.Contains(t, script, "'myservice'")
	})
	t.Run("failure", func(t *testing.T) {
		mr := newWinRunner()
		mr.AddCommandFailure(rigtest.HasPrefix("powershell.exe"), errors.New("exit 1"))
		require.Error(t, initsystem.WinSCM{}.StartService(context.Background(), mr, "myservice"))
	})
}

func TestWinSCMStopService(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mr := newWinRunner()
		mr.AddCommandSuccess(rigtest.HasPrefix("powershell.exe"))
		require.NoError(t, initsystem.WinSCM{}.StopService(context.Background(), mr, "myservice"))
		require.NoError(t, mr.NotReceived(rigtest.Contains("sc.exe")))
		script := decodePSCmd(t, mr.LastCommand())
		require.Contains(t, script, "Stop-Service")
		require.Contains(t, script, "-ErrorAction Stop")
		require.Contains(t, script, "'myservice'")
	})
	t.Run("failure", func(t *testing.T) {
		mr := newWinRunner()
		mr.AddCommandFailure(rigtest.HasPrefix("powershell.exe"), errors.New("exit 1"))
		require.Error(t, initsystem.WinSCM{}.StopService(context.Background(), mr, "myservice"))
	})
}

func TestWinSCMRestartService(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mr := newWinRunner()
		mr.AddCommandSuccess(rigtest.HasPrefix("powershell.exe"))
		require.NoError(t, initsystem.WinSCM{}.RestartService(context.Background(), mr, "myservice"))
		require.NoError(t, mr.NotReceived(rigtest.Contains("sc.exe")))
		script := decodePSCmd(t, mr.LastCommand())
		require.Contains(t, script, "Restart-Service")
		require.Contains(t, script, "-ErrorAction Stop")
		require.Contains(t, script, "'myservice'")
	})
	t.Run("failure", func(t *testing.T) {
		mr := newWinRunner()
		mr.AddCommandFailure(rigtest.HasPrefix("powershell.exe"), errors.New("exit 1"))
		require.Error(t, initsystem.WinSCM{}.RestartService(context.Background(), mr, "myservice"))
	})
}

func TestWinSCMEnableService(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mr := newWinRunner()
		mr.AddCommandSuccess(rigtest.HasPrefix("powershell.exe"))
		require.NoError(t, initsystem.WinSCM{}.EnableService(context.Background(), mr, "myservice"))
		require.NoError(t, mr.NotReceived(rigtest.Contains("sc.exe")))
		script := decodePSCmd(t, mr.LastCommand())
		require.Contains(t, script, "Set-Service")
		require.Contains(t, script, "-ErrorAction Stop")
		require.Contains(t, script, "'myservice'")
	})
	t.Run("failure", func(t *testing.T) {
		mr := newWinRunner()
		mr.AddCommandFailure(rigtest.HasPrefix("powershell.exe"), errors.New("exit 1"))
		require.Error(t, initsystem.WinSCM{}.EnableService(context.Background(), mr, "myservice"))
	})
}

func TestWinSCMDisableService(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mr := newWinRunner()
		mr.AddCommandSuccess(rigtest.HasPrefix("powershell.exe"))
		require.NoError(t, initsystem.WinSCM{}.DisableService(context.Background(), mr, "myservice"))
		require.NoError(t, mr.NotReceived(rigtest.Contains("sc.exe")))
		script := decodePSCmd(t, mr.LastCommand())
		require.Contains(t, script, "Set-Service")
		require.Contains(t, script, "-ErrorAction Stop")
		require.Contains(t, script, "'myservice'")
	})
	t.Run("failure", func(t *testing.T) {
		mr := newWinRunner()
		mr.AddCommandFailure(rigtest.HasPrefix("powershell.exe"), errors.New("exit 1"))
		require.Error(t, initsystem.WinSCM{}.DisableService(context.Background(), mr, "myservice"))
	})
}

func TestWinSCMServiceIsRunning(t *testing.T) {
	t.Run("running", func(t *testing.T) {
		mr := newWinRunner()
		mr.AddCommandSuccess(rigtest.HasPrefix("powershell.exe"))
		require.True(t, initsystem.WinSCM{}.ServiceIsRunning(context.Background(), mr, "myservice"))
		require.NoError(t, mr.NotReceived(rigtest.Contains("findstr")))
		require.NoError(t, mr.NotReceived(rigtest.Contains("sc.exe")))
		script := decodePSCmd(t, mr.LastCommand())
		require.Contains(t, script, "Get-Service")
		require.Contains(t, script, "'myservice'")
	})
	t.Run("not running", func(t *testing.T) {
		mr := newWinRunner()
		mr.AddCommandFailure(rigtest.HasPrefix("powershell.exe"), errors.New("exit 1"))
		require.False(t, initsystem.WinSCM{}.ServiceIsRunning(context.Background(), mr, "myservice"))
	})
}
