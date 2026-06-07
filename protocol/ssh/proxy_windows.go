//go:build windows

package ssh

import "os"

// proxyCommandArgs returns the argv to run a ProxyCommand string through the
// Windows command processor (COMSPEC, falling back to cmd.exe).
func proxyCommandArgs(pcmd string) []string {
	shell := os.Getenv("COMSPEC")
	if shell == "" {
		shell = "cmd.exe"
	}
	return []string{shell, "/c", pcmd}
}
