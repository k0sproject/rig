package iostream

import (
	"io"
	"sync"
)

type muxWriter struct {
	mu sync.Mutex
	w  io.Writer
}

// MuxWriter returns an io.Writer that wraps another io.Writer's writes behind a mutex to ensure only
// one write is in progress at a time.
func MuxWriter(w io.Writer) io.Writer {
	return &muxWriter{w: w}
}

func (w *muxWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Write(p) //nolint:wrapcheck
}
