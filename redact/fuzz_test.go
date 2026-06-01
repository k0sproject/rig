package redact_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/k0sproject/rig/v2/redact"
)

// FuzzStringRedacterRedact verifies that StringRedacter.Redact never panics on
// arbitrary mask, match, and input combinations, and, when match is non-empty,
// mask is shorter than match, and mask itself does not contain match, that the
// result no longer contains match.
func FuzzStringRedacterRedact(f *testing.F) {
	f.Add("[REDACTED]", "secret", "the secret is here")
	f.Add("", "password", "my password is 1234")
	f.Add("[X]", "", "nothing to redact")
	f.Add("[X]", "a", "aaa")
	f.Add("MASK", "foo", "")

	f.Fuzz(func(t *testing.T, mask, match, input string) {
		if !utf8.ValidString(mask) || !utf8.ValidString(match) || !utf8.ValidString(input) {
			return
		}
		r := redact.StringRedacter(mask, match)
		result := r.Redact(input)

		// When mask is strictly shorter than match, each ReplaceAll shrinks
		// the string so the loop terminates with all occurrences eliminated.
		// Assert full elimination in that case, provided the mask itself does
		// not contain match (which would be self-defeating).
		// When len(mask) == len(match), a bounded loop is used; residuals are
		// possible if the bound is hit. When len(mask) > len(match), a single
		// pass is used to avoid unbounded string growth; residuals are possible.
		if match != "" && len(mask) < len(match) && !strings.Contains(mask, match) && strings.Contains(result, match) {
			t.Errorf("Redact(%q) with match=%q mask=%q: result %q still contains the match", input, match, mask, result)
		}
	})
}

// FuzzStringRedacterMultipleMatches verifies that StringRedacter never panics when
// given multiple match strings, including overlapping ones. No post-condition is
// checked because sequential strings.ReplaceAll can produce residual matches when
// mask and match strings overlap (e.g. mask="0", matches=["00","000"]).
func FuzzStringRedacterMultipleMatches(f *testing.F) {
	f.Add("[X]", "foo", "bar")
	f.Add("", "a", "b")
	f.Add("0", "00", "000")

	f.Fuzz(func(t *testing.T, mask, match1, match2 string) {
		if !utf8.ValidString(mask) || !utf8.ValidString(match1) || !utf8.ValidString(match2) {
			return
		}
		r := redact.StringRedacter(mask, match1, match2)
		input := match1 + " and " + match2 + " mixed"
		// Must not panic; overlapping patterns may produce residual matches.
		_ = r.Redact(input)
	})
}
