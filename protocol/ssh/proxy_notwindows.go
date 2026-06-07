//go:build !windows

package ssh

import (
	"os"
	"strings"
)

// proxyCommandArgs returns the argv to run a ProxyCommand string through the
// user's shell ($SHELL, falling back to sh). "exec" is prepended so the shell
// replaces itself with the child process, ensuring process kill reaches the
// actual proxy and not a lingering shell wrapper. The prefix is skipped when
// the first token is already "exec" to avoid producing "exec exec ...".
//
// Limitation: prepending "exec" changes shell semantics for leading environment
// variable assignments (e.g. "FOO=bar nc %h %p" becomes "exec FOO=bar ..." which
// treats the assignment as a command name rather than an env var). Use
// "env FOO=bar nc %h %p" as a portable alternative in ProxyCommand values.
func proxyCommandArgs(pcmd string) []string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "sh"
	}
	cmd := "exec " + pcmd
	if first := strings.Fields(pcmd); len(first) > 0 && first[0] == "exec" {
		cmd = pcmd
	}
	return []string{shell, "-c", cmd}
}
