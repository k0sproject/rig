package iostream

import (
	"bytes"
	"errors"
	"io"

	"github.com/k0sproject/rig/byteslice"
)

var ErrUnexpected = errors.New("redact reader unexpected error")
var BufferSize = 1024

type RedactReader struct {
	r     io.Reader
	match []byte
	work  []byte
	buf   bytes.Buffer
	mask  []byte
	isEOF bool
}

func NewRedactReader(r io.Reader, match, mask []byte) *RedactReader {
	return &RedactReader{
		r:     r,
		match: match,
		work:  make([]byte, BufferSize),
		mask:  mask,
	}
}

func (rr *RedactReader) Read(p []byte) (int, error) {
	// if we have data in the buffer, need to resolve if there are matches or partial matches
	if rr.buf.Len() > 0 {
		return rr.resolve(p)
	}

	if rr.isEOF {
		return 0, io.EOF
	}

	// read new data into the buffer
	if err := rr.readToBuffer(); err != nil {
		return 0, err
	}

	// start from the beginning
	return rr.Read(p)
}

func (rr *RedactReader) readToBuffer() error {
	if rr.isEOF {
		return io.EOF
	}

	// read new data into the work buffer
	n, err := rr.r.Read(rr.work)
	if n == 0 || err != nil {
		rr.isEOF = true
		return err
	}

	// append it to the main buffer
	_, _ = rr.buf.Write(rr.work[:n])

	// replace full matches
	newBytes := bytes.ReplaceAll(rr.buf.Bytes(), rr.match, rr.mask)

	// replace main buffer contents
	rr.buf.Reset()
	_, _ = rr.buf.Write(newBytes)

	return err
}

func (rr *RedactReader) resolve(p []byte) (int, error) {
	for {
		if err := rr.readToBuffer(); err != nil && !errors.Is(err, io.EOF) {
			return 0, err
		}

		// if there are no partial matches or the reader has reached eof, return the buffer contents
		if byteslice.PartialIndex(rr.buf.Bytes(), rr.match) == -1 || rr.isEOF {
			return rr.buf.Read(p)
		}
	}
}
