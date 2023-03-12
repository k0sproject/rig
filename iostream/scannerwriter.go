package iostream

import (
	"bytes"
	"io"
)

// ScanWriterMaxBufferSize is the maximum size of the ScanWriter buffer
var ScanWriterMaxBufferSize = 1024 * 1024

type scannerWriter struct {
	buf    bytes.Buffer
	fn     CallbackFn
	delim  byte
	closed bool
}

type CallbackFn func(string)

// ScanWriter returns an io.WriteCloser that calls the given callback function with the
// contents of the internal buffer every time it encounters the given delimiter. The remaining
// buffer contents are flushed when the writer is closed or ScanWriterMaxBufferSize is reached.
// It's like a bufio.Scanner wrapped into an io.Writer.
func ScanWriter(delim byte, fn CallbackFn) io.WriteCloser {
	return &scannerWriter{fn: fn, delim: delim}
}

func (w *scannerWriter) Write(p []byte) (int, error) {
	if w.closed {
		return 0, io.ErrUnexpectedEOF
	}

	// don't let the internal buffer grow too large, flush buffer contents
	if w.buf.Len()+len(p) > ScanWriterMaxBufferSize {
		w.fn(w.buf.String())
		w.buf.Reset()
	}

	writeN, writeErr := w.buf.Write(p)

	w.scan()

	return writeN, writeErr
}

func (w *scannerWriter) scan() {
	for {
		line, err := w.buf.ReadString(w.delim)
		if err != nil {
			w.buf.Write([]byte(line))
			break
		}
		w.fn(line)
	}
}

func (w *scannerWriter) Close() error {
	if w.closed {
		return io.ErrUnexpectedEOF
	}
	if w.buf.Len() > 0 {
		w.scan()
	}
	if w.buf.Len() > 0 {
		w.fn(w.buf.String())
	}
	w.buf.Reset()
	w.closed = true
	return nil
}
