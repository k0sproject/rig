package rig

import (
	"bytes"
	"io"
	"regexp"
	"strings"
	"sync"

	"golang.org/x/text/transform"
)

type StringRedacter struct {
	Match string
	Mask  string
	sb *strings.Builder
	partial *bytes.Buffer
}

func NewStringRedacter(match string, mask string) StringRedacter {
	return StringRedacter{Match: match, Mask: mask}
}

func (s *StringRedacter) Transform(dst, src []byte, atEOF bool) (nDst, nSrc int, err error) {
	s.sb.Reset()
	s.sb.Grow(len(src) + s.partial.Len())
	if s.partial.Len() > 0 {
		_, _ = io.Copy(s.sb, s.partial)
		s.partial.Reset()
	}
	s.sb.Write(src)
	input := s.sb.String()
	replaced := strings.ReplaceAll(input, s.Match, s.Mask)
	nDst = copy(dst, replaced)

	if nDst < len(replaced) {
		// Not all data could be copied to dst. Store the rest for the next Transform call
		s.partial.WriteString(replaced[nDst:])
		return nDst, len(input), nil
	}

	nSrc = len(src)
	if !atEOF && len(src) >= len(sr.old) {
		// Handle the case where the word to replace might be split
		overlap := len(sr.old) - 1
		sr.partial.WriteString(string(src[len(src)-overlap:]))
		nSrc -= overlap
	}





	input := string(src)
	replaced := strings.ReplaceAll(input, s.Match, s.Mask)
	copy(dst, replaced)
	nDst = len(replaced)
	nSrc = len(input)

	if len(replaced) > len(dst) {
		return 0, 0, transform.ErrShortDst
	}

	return nDst, nSrc, nil
}

func (s StringRedacter) Reset() {}

type RegexpRedacter struct {
	Match *regexp.Regexp
	Mask  string
}

func (r RegexpRedacter) Redact(input string) string {
	return r.Match.ReplaceAllString(input, r.Mask)
}

func NewRegexpRedacter(regex *regexp.Regexp, mask string) RegexpRedacter {
	return RegexpRedacter{Match: regex, Mask: mask}
}

// a writer that hides sensitive data from the output.
type RedactingWriter struct {
	w         io.Writer
	redacters []Redacter
}

var redactBufPool = sync.Pool{
	New: func() any {
		return bytes.NewBuffer(nil)
	},
}

func (r RedactingWriter) Write(p []byte) (int, error) {
	b, ok := redactBufPool.Get().(*bytes.Buffer)
	if !ok {
		b = bytes.NewBuffer(nil)
	}
	defer redactBufPool.Put(b)
	b.Grow(len(p))

	for _, redacter := range r.redacters {
		b.Reset()
		_, _ = b.WriteString(redacter.Redact(string(b.Bytes())))
	}
	_, err := r.w.Write(b.Bytes())
	if err != nil {
		return 0, err //nolint:wrapcheck // don't want to change the error
	}
	return len(p), nil
}
