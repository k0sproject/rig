package exec

import "fmt"

// NopSudo is a noop sudo method for root users
type NopSudo struct{}

func (n NopSudo) New(r *Runner, _ PasswordCallback) (SudoFn, error) {
	var cmd string
	if r.client.IsWindows() {
		cmd = "net session >nul"
	} else {
		cmd = `[ "$(id -u)" = 0 ]`
	}

	if err := r.Exec(cmd); err != nil {
		return nil, fmt.Errorf("already privileged check failed: %w", err)
	}

	return n.Sudo, nil
}

func (n NopSudo) Sudo(cmd string) (string, error) {
	return cmd, nil
}
