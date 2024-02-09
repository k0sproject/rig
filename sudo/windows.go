package sudo

import (
	"strings"

	"github.com/k0sproject/rig/exec"
)

// RegisterWindowsNoop registers a noop DecorateFunc with the given repository if the user is root.
func RegisterWindowsNoop(repository *Repository) {
	repository.Register(func(runner exec.SimpleRunner) exec.DecorateFunc {
		if !runner.IsWindows() {
			return nil
		}
		out, err := runner.ExecOutput(`whoami.exe`)
		if err != nil {
			return nil
		}
		parts := strings.Split(out, `\`)
		if strings.ToLower(parts[len(parts)-1]) == "administrator" {
			// user is already the administrator
			return Noop
		}

		if runner.Exec(`cmd.exe /c 'net user "%USERNAME%" | findstr /B /C:"Local Group Memberships" | findstr /C:"*Administrators"'`) != nil {
			// user is not in the Administrators group
			return nil
		}

		out, err = runner.ExecOutput(`reg.exe query "HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\System" /v "EnableLUA"`)
		if err != nil {
			// failed to query if UAC is enabled
			return nil
		}

		if strings.Contains(out, "0x0") {
			// UAC is disabled and the user is in the Administrators group
			return Noop
		}

		// UAC is enabled and user is not 'Administrator'"
		return nil
	})
}
