package redact

import (
	"bytes"
	"io"
	"sort"

	"github.com/k0sproject/rig/byteslice"
)

type redactWriter struct {
	w       io.Writer
	matches [][]byte
	mask    []byte
	buf     *bytes.Buffer
	out     *bytes.Buffer
	closed  bool
}

func Writer(w io.Writer, mask []byte, matches ...[]byte) io.WriteCloser {
	return &redactWriter{
		w:       w,
		matches: matches,
		mask:    mask,
		buf:     &bytes.Buffer{},
		out:     &bytes.Buffer{},
	}
}

func (rw *redactWriter) Write(p []byte) (int, error) {
	if rw.closed {
		return 0, io.ErrClosedPipe
	}

	// Write the input data directly to the buffer.
	_, err := rw.buf.Write(p)
	if err != nil {
		return 0, err
	}

	// Redact data from the buffer into the output buffer.
	if err := rw.redactToBuffer(); err != nil {
		return 0, err
	}

	// Copy any redacted data from the output buffer to the underlying writer.
	if rw.out.Len() > 0 {
		_, err = io.Copy(rw.w, rw.out)
	}

	return len(p), err
}

func (rw *redactWriter) Flush() error {
	_, err := io.Copy(rw.w, rw.out)
	return err
}

func (rw *redactWriter) Close() error {
	rw.closed = true
	// Flush any remaining data from the out buffer.
	if err := rw.Flush(); err != nil {
		return err
	}

	// Copy any remaining data from the buffer to the underlying writer.
	// (should be just a partial match that didn't get to complete)
	_, err := io.Copy(rw.w, rw.buf)
	return err
}

type matchInfo struct {
	start int
	end   int
}

func (rw *redactWriter) redactToBuffer() error {
	var err error

	// Find all matches
	var matches []matchInfo
	firstPartial := -1
	for _, pattern := range rw.matches {
		indexes, partial := byteslice.IndexAllPartial(rw.buf.Bytes(), pattern)
		for _, index := range indexes {
			matches = append(matches, matchInfo{start: index, end: index + len(pattern)})
		}
		if partial != -1 && (firstPartial == -1 || partial < firstPartial) {
			firstPartial = partial
		}
	}

	if len(matches) == 0 && rw.buf.Len() > 0 {
		if firstPartial == -1 {
			// no matches, no partial, copy it all
			_, err = io.Copy(rw.out, rw.buf)
		} else if firstPartial > 0 {
			// Leave partial match in buffer
			_, err = io.CopyN(rw.out, rw.buf, int64(firstPartial))
		}
		return err
	}

	// Sort matches by start index
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].start < matches[j].start
	})

	// Redact matches
	lastEnd := 0
	for _, match := range matches {
		if match.start < lastEnd {
			continue
		}
		// Copy bytes before the match
		if match.start > lastEnd {
			_, err = io.CopyN(rw.out, rw.buf, int64(match.start-lastEnd))
		}
		// Redact the match
		rw.out.Write(rw.mask)

		// discard the match
		rw.buf.Next(match.end - match.start)

		lastEnd = match.end
	}

	if firstPartial != -1 {
		// Leave partial match in buffer
		if len(matches) > 0 && lastEnd < firstPartial {
			_, err = io.CopyN(rw.out, rw.buf, int64(firstPartial-lastEnd))
		}
	} else if rw.buf.Len() > 0 {
		// No partial match, copy all of the buffer to the output
		_, err = io.Copy(rw.out, rw.buf)
	}

	return err
}
