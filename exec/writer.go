package exec

import (
	"io"
	"strings"
)

type redactingWriter struct {
	w  io.Writer
	fn func(string) string
}

func (r redactingWriter) Write(p []byte) (int, error) {
	s := r.fn(string(p))
	_, err := r.w.Write([]byte(s))
	if err != nil {
		return 0, err //nolint:wrapcheck // don't want to change the error
	}
	return len(p), nil
}

type logWriter struct {
	fn func(string, ...any)
}

func (l logWriter) Write(p []byte) (int, error) {
	s := string(p)
	l.fn(strings.ReplaceAll(s, "%", "%%"))
	return len(p), nil
}

type flaggingWriter struct {
	b *bool
}

func (f *flaggingWriter) Write(p []byte) (int, error) {
	if !*f.b && len(p) > 0 {
		*f.b = true
	}
	return len(p), nil
}
