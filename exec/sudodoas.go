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

func (s SudoDoas) Check(r *Runner) bool {
	if r.IsWindows() {
		return false
	}
	return r.Execf("%s -n true", s.command) == nil
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
