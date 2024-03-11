package sudo

import (
	"strings"

	"github.com/k0sproject/rig/v2/cmd"
)

// RegisterWindowsNoop registers a noop DecorateFunc with the given repository if the user is root.
func RegisterWindowsNoop(repository *Provider) {
	repository.Register(func(runner cmd.Runner) (cmd.Runner, bool) {
		if !runner.IsWindows() {
			return nil, false
		}
		out, err := runner.ExecOutput(`whoami.exe`)
		if err != nil {
			return nil, false
		}
		parts := strings.Split(out, `\`)
		if strings.ToLower(parts[len(parts)-1]) == "administrator" {
			// user is already the administrator
			return runner, true
		}

		if runner.Exec(`cmd.exe /c 'net user "%USERNAME%" | findstr /B /C:"Local Group Memberships" | findstr /C:"*Administrators"'`) != nil {
			// user is not in the Administrators group
			return nil, false
		}

		out, err = runner.ExecOutput(`reg.exe query "HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\System" /v "EnableLUA"`)
		if err != nil {
			// failed to query if UAC is enabled
			return nil, false
		}

		if strings.Contains(out, "0x0") {
			// UAC is disabled and the user is in the Administrators group
			return runner, true
		}

		// UAC is enabled and user is not 'Administrator'"
		return nil, false
	})
}
