package redact

import (
	"io"
	"strings"
)

// Redacter is implemented by types that can redact sensitive information from a string.
type Redacter interface {
	Redact(input string) string
	Reader(src io.Reader) io.Reader
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

func StringRedacter(mask string, matches ...string) Redacter {
	if len(matches) == 0 {
		return noopRedacter{}
	}
	var newMatches []string //nolint:prealloc
	for _, match := range matches {
		if match == "" {
			continue
		}
		for _, m := range matches {
			if m == match {
				continue
			}
		}
		newMatches = append(newMatches, match)
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
	}
	return s
}

func (r *stringRedacter) Reader(src io.Reader) io.Reader {
	return Reader(src, r.mask, r.matches...)
}

func (r *stringRedacter) Writer(src io.Writer) io.WriteCloser {
	return Writer(src, r.mask, r.matches...)
}
