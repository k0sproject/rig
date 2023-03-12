package exec

// There doesn't seem to be a way to "sudo" in windows without a password.
// This expects the user to already be elevated.
// TODO: consider a password prompting version of this.
type WindowsNop struct{}

func (w WindowsNop) Check(r *Runner) bool {
	if !r.IsWindows() {
		return false
	}
	return r.Exec(`net session >nul`) == nil
}

func (w WindowsNop) Sudo(cmd string) (string, error) {
	return cmd, nil
}
