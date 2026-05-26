package sshconfig_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/k0sproject/rig/v2/sshconfig"
)

// FuzzNewParser verifies that the ssh_config parser never panics on arbitrary input.
// Include directives are stripped to avoid filesystem access during fuzzing.
func FuzzNewParser(f *testing.F) {
	f.Add("Host *\n  Port 22\n  User root\n")
	f.Add("Host example.com\n  IdentityFile ~/.ssh/id_rsa\n  StrictHostKeyChecking no\n")
	f.Add("")
	f.Add("# comment only\n")
	f.Add("Host *.internal\n  ProxyJump bastion\n")
	f.Add("Match User root\n  PermitRootLogin yes\n")
	f.Add("Host bad\n  BadKey InvalidValue\n")

	f.Fuzz(func(t *testing.T, input string) {
		if !utf8.ValidString(input) {
			return
		}
		// Strip Include directives to prevent filesystem access during fuzzing.
		var filtered strings.Builder
		for line := range strings.SplitSeq(input, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(strings.ToLower(trimmed), "include") {
				continue
			}
			filtered.WriteString(line)
			filtered.WriteByte('\n')
		}

		parser, err := sshconfig.NewParser(strings.NewReader(filtered.String()))
		if err != nil {
			// Syntax errors are expected; panics are not.
			return
		}
		cfg := &sshconfig.Config{}
		// Apply is also exercised; errors are acceptable.
		_ = parser.Apply(cfg, "fuzz-host")
	})
}
