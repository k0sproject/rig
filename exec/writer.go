package exec

import (
	"io"
	"strings"
)

// a writer that hides sensitive data from the output
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

// a writer that calls a logging function for each line written
type logWriter struct {
	fn func(string, ...any)
}

// Write writes the given bytes to the log function
func (l logWriter) Write(p []byte) (int, error) {
	s := string(p)
	l.fn(strings.ReplaceAll(s, "%", "%%"))
	return len(p), nil
}

// flaggingWriter is a discarding writer that sets a flag when it writes something, used
// to check if a command has output to stderr
type flaggingWriter struct {
	b *bool
}

func (f *flaggingWriter) Write(p []byte) (int, error) {
	if !*f.b && len(p) > 0 {
		*f.b = true
	}
	return len(p), nil
}
