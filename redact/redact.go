// Package redact provides redaction of sensitive information from strings or streams.
package redact

import (
	"io"
	"strings"
)

// Redacter is implemented by types that can redact sensitive information from a string.
type Redacter interface {
	Redact(input string) string
}

type noopWriteCloser struct {
	io.Writer
}

func (noopWriteCloser) Close() error {
	return nil
}

type noopRedacter struct{}

func (r noopRedacter) Redact(s string) string         { return s }
func (r noopRedacter) Reader(src io.Reader) io.Reader { return src }
func (r noopRedacter) Writer(dst io.Writer) io.WriteCloser {
	if w, ok := dst.(io.WriteCloser); ok {
		return w
	}
	return noopWriteCloser{dst}
}

// StringRedacter returns a Redacter that performs best-effort redaction of the provided match strings using the
// provided mask. Redaction is not guaranteed to be complete in all cases:
//   - Matches where the mask contains the match string are silently dropped (replacing would immediately reintroduce the secret).
//   - When the mask is longer than the match, only a single replacement pass is made; a replacement can re-introduce
//     the match at a boundary (e.g. match="ab", mask="xxa", input="abb" → "xxab"), leaving a residual occurrence.
//
// Callers must not rely on this for guaranteed removal of sensitive data. It is intended for best-effort log redaction.
func StringRedacter(mask string, matches ...string) Redacter {
	if len(matches) == 0 {
		return noopRedacter{}
	}
	var newMatches []string
	for _, match := range matches {
		if match == "" {
			continue
		}
		if strings.Contains(mask, match) {
			// Replacing this match with the mask would reintroduce it; skip.
			continue
		}
		newMatches = append(newMatches, match)
	}
	if len(newMatches) == 0 {
		return noopRedacter{}
	}
	return &stringRedacter{newMatches, mask}
}

type stringRedacter struct {
	matches []string
	mask    string
}

func (r *stringRedacter) Redact(s string) string {
	for _, match := range r.matches {
		s = strings.ReplaceAll(s, match, r.mask)
		if len(r.mask) > len(match) {
			// mask longer than match: looping can re-introduce match at
			// replacement boundaries and grow the string without bound;
			// one pass is used. Residual occurrences are possible.
			continue
		}
		// mask same length or shorter: additional passes clear boundary
		// re-introductions. The loop is bounded by len(s) to avoid
		// non-termination in pathological equal-length mask/match cases.
		for i := len(s); i > 0 && strings.Contains(s, match); i-- {
			s = strings.ReplaceAll(s, match, r.mask)
		}
	}
	return s
}
