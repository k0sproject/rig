package iostream

import (
	"io"
	"sync"
)

// CallbackDiscard is an io.Writer that calls a callback function once when or if
// non-whitespace data is written to it. This can be used in conjunction with
// io.MultiWriter to help detect if any data was written to a stream. All data
// written to the writer is discarded.
func CallbackDiscard(fn func()) io.Writer {
	return &callbackDiscard{fn: fn}
}

type callbackDiscard struct {
	r    bool
	fn   func()
	once sync.Once
}

func (w *callbackDiscard) Write(p []byte) (int, error) {
	if w.r {
		// already served its purpose
		return len(p), nil
	}
	for _, c := range p {
		switch c {
		case ' ', '\t', '\n', '\r', '\f', '\v':
			// ignore whitespace
		default:
			w.r = true
			w.once.Do(w.fn)
			return len(p), nil
		}
	}
	return len(p), nil
}
