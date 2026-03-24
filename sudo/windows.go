package sudo

import (
	"strings"

	"github.com/k0sproject/rig/v2/cmd"
)

// RegisterWindowsNoop registers a noop DecorateFunc with the given repository if the user is
// the built-in Administrator or a member of the Administrators group with UAC disabled.
func RegisterWindowsNoop(repository *Registry) {
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

		// Check if the current token includes the Administrators group SID.
		// When UAC is enabled and the session is not elevated, the token is filtered
		// and will not contain this SID even if the user is a group member.
		adminSID := `[Security.Principal.SecurityIdentifier]::new([Security.Principal.WellKnownSidType]::BuiltinAdministratorsSid, $null)`
		if runner.Exec(`if (-not ([Security.Principal.WindowsIdentity]::GetCurrent().Groups -contains `+adminSID+`)) { exit 1 }`, cmd.PS()) != nil {
			return nil, false
		}

		return runner, true
	})
}
