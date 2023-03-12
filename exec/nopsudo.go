package exec

// NopSudo is a noop sudo method for root users
type NopSudo struct{}

func (n NopSudo) Check(r *Runner) bool {
	if r.IsWindows() {
		return false
	}
	return r.Exec(`[ "$(id -u)" = 0 ]`) == nil
}

func (n NopSudo) Sudo(cmd string) (string, error) {
	return cmd, nil
}
