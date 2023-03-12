package iostream

import (
	"io"
	"strings"
	"sync"
)

// NopCallbackWriter is a writer that calls a callback function once if
// non-whitespace data is written to it. This can be used in conjunction with
// io.MultiWriter to help detect if any data was written to a stream.
func NopCallbackWriter(fn func()) io.Writer {
	io.MultiWriter()
	return &nopCallbackWriter{Fn: fn}
}

type nopCallbackWriter struct {
	dataReceived bool
	Fn           func()
	once         sync.Once
}

func (w *nopCallbackWriter) Write(p []byte) (int, error) {
	if !w.dataReceived {
		w.dataReceived = strings.TrimSpace(string(p)) != ""
		if w.dataReceived && w.Fn != nil {
			w.once.Do(w.Fn)
		}
	}

	return len(p), nil
}
