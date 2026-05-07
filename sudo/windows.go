package sudo

import (
	"github.com/k0sproject/rig/v2/cmd"
)

// RegisterWindowsNoop registers a noop DecorateFunc with the given repository if the current
// session has effective administrator privileges. IsInRole uses CheckTokenMembership, which
// returns true only when the Administrators SID is present and not marked deny-only — the
// correct signal for an effectively elevated token. SSH sessions on Windows always provide a
// full elevated token for Administrators group members regardless of UAC; WinRM does too for
// domain accounts or when LocalAccountTokenFilterPolicy=1 is set.
func RegisterWindowsNoop(repository *Registry) {
	repository.Register(func(runner cmd.Runner) (cmd.Runner, bool) {
		if !runner.IsWindows() {
			return nil, false
		}
		const isAdminCmd = `if (-not (New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) { exit 1 }`
		if runner.Exec(isAdminCmd, cmd.PS()) != nil {
			return nil, false
		}
		return cmd.NewExecutor(runner, Noop), true
	})
}
