package redact

import (
	"bytes"
	"errors"
	"io"

	"github.com/k0sproject/rig/byteslice"
)

type redactReader struct {
	r        io.Reader
	match    []byte
	buf      *bytes.Buffer
	out      *bytes.Buffer
	mask     []byte
	isEOF    bool
	matchLen int64
}

func Reader(r io.Reader, match, mask []byte) *redactReader {
	return &redactReader{
		r:        r,
		match:    match,
		buf:      &bytes.Buffer{},
		out:      &bytes.Buffer{},
		mask:     mask,
		matchLen: int64(len(match)),
	}
}

func (rr *redactReader) Read(p []byte) (int, error) {
	if rr.isEOF && rr.out.Len() == 0 {
		return 0, io.EOF
	}

	return rr.resolve(p)
}

func (rr *redactReader) redactToBuffer(p []byte) error {
	if rr.isEOF {
		// nothing more to read or redact
		if rr.buf.Len() > 0 {
			_, _ = io.Copy(rr.out, rr.buf)
		}
		return nil
	}

	// Read data into the buffer from the underlying reader, up to the length of the caller's buffer or 2x the length of the match, which ever is greater
	n, err := io.CopyN(rr.buf, rr.r, max(int64(len(p)), 2*rr.matchLen))
	if err != nil {
		if !errors.Is(err, io.EOF) {
			return err
		}

		rr.isEOF = true
	}

	if n == 0 {
		return nil
	}

	// Find indexes for all matches in the buffer, even any lingering partial match
	indexes, partial := byteslice.IndexAllPartial(rr.buf.Bytes(), rr.match)

	var pos int64

	for _, index := range indexes {
		if pos < int64(index) {
			// Copy data preceding the match
			n, err = io.CopyN(rr.out, rr.buf, int64(index)-pos)
			if err != nil && !errors.Is(err, io.EOF) {
				return err
			}
			pos += n
		}

		// Write the mask for the match
		_, err = rr.out.Write(rr.mask)
		if err != nil {
			return err
		}

		// Advance the buf past the match by discarding bytes
		rr.buf.Next(len(rr.match))
		pos += int64(len(rr.match))
	}

	if partial == -1 || rr.isEOF {
		// Copy the remainder of the reader to the buffer
		_, err = io.Copy(rr.out, rr.buf)
	} else {
		// Only copy up until the partial match and leave it in the buffer
		_, err = io.CopyN(rr.out, rr.buf, int64(rr.buf.Len()-partial-1))
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}

	return nil
}

func (rr *redactReader) resolve(p []byte) (int, error) {
	for {
		if rr.out.Len() > 0 {
			// There's data in the output buffer, so let them have it
			n, err := rr.out.Read(p)
			if err != nil && !errors.Is(err, io.EOF) {
				return n, err
			}
			return n, nil
		}

		if err := rr.redactToBuffer(p); err != nil {
			return 0, err
		}

		if rr.out.Len() == 0 && rr.isEOF && rr.buf.Len() == 0 {
			// Nothing left to do
			return 0, io.EOF
		}
	}
}
