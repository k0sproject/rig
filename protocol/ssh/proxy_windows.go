//go:build windows

package ssh

import (
	"fmt"
	"os"
	"os/exec"
)

// buildKillFunc returns a function that terminates the proxy process tree on
// Windows. Killing proc.Process only terminates cmd.exe, leaving its children
// (the actual proxy program) as orphans. taskkill /F /T terminates the full
// process tree; proc.Process.Kill() is called as a fallback.
func buildKillFunc(proc *exec.Cmd) func() {
	return func() {
		pid := proc.Process.Pid
		_ = exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", pid)).Run() //nolint:noctx,gosec // pid is an integer, no injection risk; no ctx needed for a best-effort cleanup call
		_ = proc.Process.Kill()
		go proc.Wait() //nolint:errcheck
	}
}

// proxyCommandArgs returns the argv to run a ProxyCommand string through the
// Windows command processor (COMSPEC, falling back to cmd.exe).
func proxyCommandArgs(pcmd string) []string {
	shell := os.Getenv("COMSPEC")
	if shell == "" {
		shell = "cmd.exe"
	}
	return []string{shell, "/c", pcmd}
}
