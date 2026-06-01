package sshconfig_test

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/k0sproject/rig/v2/sshconfig"
)

type noopExecutor struct{}

func (noopExecutor) Run(_ string, _ ...string) error { return nil }

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
		// Only skip lines whose first whitespace/= delimited token is exactly "include"
		// (case-insensitive), so that lines like "IncludeMe yes" still reach the parser.
		var filtered strings.Builder
		for line := range strings.SplitSeq(input, "\n") {
			trimmed := strings.TrimSpace(line)
			// Extract the first token (delimited by whitespace or '=').
			firstToken := trimmed
			if i := strings.IndexFunc(trimmed, func(r rune) bool { return unicode.IsSpace(r) || r == '=' }); i >= 0 {
				firstToken = trimmed[:i]
			}
			if strings.EqualFold(firstToken, "include") {
				continue
			}
			filtered.WriteString(line)
			filtered.WriteByte('\n')
		}

		parser, err := sshconfig.NewParser(strings.NewReader(filtered.String()), sshconfig.WithExecutor(noopExecutor{}))
		if err != nil {
			// Syntax errors are expected; panics are not.
			return
		}
		cfg := &sshconfig.Config{}
		// Apply is also exercised; errors are acceptable.
		_ = parser.Apply(cfg, "fuzz-host")
	})
}
