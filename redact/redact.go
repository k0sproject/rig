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

type noopRedacter struct{}

func (r noopRedacter) Redact(s string) string         { return s }
func (r noopRedacter) Reader(src io.Reader) io.Reader { return src }

func StringRedacter(match string, mask string) Redacter {
	if len(match) == 0 {
		return noopRedacter{}
	}
	return &stringRedacter{match, mask}
}

type stringRedacter struct {
	match string
	mask  string
}

func (r *stringRedacter) Redact(s string) string {
	return strings.ReplaceAll(s, r.match, r.mask)
}

func (r *stringRedacter) Reader(src io.Reader) io.Reader {
	return Reader(src, []byte(r.match), []byte(r.mask))
}
