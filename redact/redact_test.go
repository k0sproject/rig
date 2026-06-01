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
		{
			// When the mask itself contains the match string, replacing would
			// immediately reintroduce the secret; StringRedacter silently drops
			// such matches so the returned Redacter is a no-op for that secret.
			name:     "mask contains match is dropped",
			redacter: redact.StringRedacter("(secret)", "secret"),
			input:    "the password is secret",
			expected: "the password is secret",
		},
		{
			// Only matches whose mask doesn't contain them are dropped; others
			// still work normally.
			name:     "mask contains one of multiple matches",
			redacter: redact.StringRedacter("(secret)", "secret", "password"),
			input:    "the password is secret",
			expected: "the (secret) is secret",
		},
		{
			// A single ReplaceAll pass can create new occurrences of match at
			// replacement boundaries when mask and match are the same length;
			// additional passes are needed to clear them.
			name:     "equal-length mask boundary re-introduction",
			redacter: redact.StringRedacter("ba", "ab"),
			input:    "aabb",
			expected: "bbaa",
		},
		{
			// When mask is longer than match, only one pass is made to avoid
			// unbounded string growth. A replacement can re-introduce match at
			// a boundary; that residual occurrence is intentional/documented.
			name:     "longer mask single-pass residual is intentional",
			redacter: redact.StringRedacter("xxa", "ab"),
			input:    "abb",
			expected: "xxab",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, test.redacter.Redact(test.input))
		})
	}
}
