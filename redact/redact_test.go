package redact_test

import (
	"strings"
	"testing"

	"github.com/k0sproject/rig/v2/redact"
	"github.com/stretchr/testify/assert"
)

func TestStringRedacter(t *testing.T) {
	tests := []struct {
		name     string
		redacter redact.Redacter
		input    string
		expected string
	}{
		{
			name:     "simple",
			redacter: redact.StringRedacter("REDACTED", "ken sent me"),
			input:    "the password is ken sent me",
			expected: "the password is REDACTED",
		},
		{
			name:     "empty",
			redacter: redact.StringRedacter("REDACTED", ""),
			input:    "the password is ken sent me",
			expected: "the password is ken sent me",
		},
		{
			name:     "empty input",
			redacter: redact.StringRedacter("REDACTED", "ken sent me"),
			input:    "",
			expected: "",
		},
		{
			name:     "empty input and redact",
			redacter: redact.StringRedacter("", ""),
			input:    "",
			expected: "",
		},
		{
			name:     "empty mask",
			redacter: redact.StringRedacter("", "ken sent me"),
			input:    "the password is ken sent me",
			expected: "the password is ",
		},
		{
			name:     "no match",
			redacter: redact.StringRedacter("REDACTED", "ken sent me"),
			input:    "the password is not here",
			expected: "the password is not here",
		},
		{
			name:     "multiple matches",
			redacter: redact.StringRedacter("REDACTED", "secret"),
			input:    "secret password secret secret password",
			expected: "REDACTED password REDACTED REDACTED password",
		},
		{
			name:     "a lot of matches",
			redacter: redact.StringRedacter("REDACTED", "test"),
			input:    "foo" + strings.Repeat("test", 1000) + "bar",
			expected: "foo" + strings.Repeat("REDACTED", 1000) + "bar",
		},
		{
			name:     "multiple matchers",
			redacter: redact.StringRedacter(".", "e", "w"),
			input:    "secret password secret secret password",
			expected: "s.cr.t pass.ord s.cr.t s.cr.t pass.ord",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, test.redacter.Redact(test.input))
		})
	}
}
