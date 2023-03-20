package exec

import (
	"fmt"
	"strings"

	"github.com/alessio/shellescape"
	"github.com/google/shlex"
)

// SudoDoas is a sudo method for "sudo" or "doas" commands
type SudoDoas struct {
	command string
}

var ErrNotSupportedOnWindows = fmt.Errorf("not supported on windows")

func (s SudoDoas) New(r *Runner, _ PasswordCallback) (SudoFn, error) {
	if r.IsWindows() {
		return nil, ErrNotSupportedOnWindows
	}
	if err := r.Execf("%s -n true", s.command); err != nil {
		return nil, fmt.Errorf("%s check failed: %w", s.command, err)
	}
	return s.Sudo, nil
}

func (s SudoDoas) Sudo(cmd string) (string, error) {
	parts, err := shlex.Split(cmd)
	if err != nil {
		return "", fmt.Errorf("parse command for sudo: %w", err)
	}

	for i, p := range parts {
		parts[i] = shellescape.Quote(p)
	}

	return fmt.Sprintf("%s -n -s %s -- %s", s.command, parts[0], strings.Join(parts[1:], " ")), nil
}
