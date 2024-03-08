package redact

import (
	"bytes"
	"errors"
	"io"
	"sort"

	"github.com/k0sproject/rig/v2/byteslice"
)

type redactReader struct {
	r       io.Reader
	matches [][]byte
	buf     *bytes.Buffer
	out     *bytes.Buffer
	mask    []byte
	isEOF   bool
	maxLen  int64
}

// Reader returns a new io.Reader that will redact any matches of the provided strings with the provided mask.
func Reader(r io.Reader, mask string, matches ...string) io.Reader {
	matchBytes := make([][]byte, len(matches))
	var maxLen int
	for i, match := range matches {
		matchBytes[i] = []byte(match)
		maxLen = max(maxLen, len(matchBytes[i]))
	}
	return &redactReader{
		r:       r,
		matches: matchBytes,
		buf:     &bytes.Buffer{},
		out:     &bytes.Buffer{},
		mask:    []byte(mask),
		maxLen:  int64(maxLen),
	}
}

// Read implements the io.Reader interface.
func (rr *redactReader) Read(p []byte) (int, error) {
	if rr.isEOF && rr.out.Len() == 0 {
		return 0, io.EOF
	}

	return rr.resolve(p)
}

func (rr *redactReader) resolve(p []byte) (int, error) {
	for {
		if rr.out.Len() > 0 {
			// There's data in the output buffer, so let them have it all
			n, err := rr.out.Read(p)
			if err != nil && !errors.Is(err, io.EOF) {
				return n, err //nolint:wrapcheck
			}
			return n, nil
		}

		// Read data into the buffer from the underlying reader, up to the length of the caller's
		// buffer or 2x the length of the max match, which ever is greater
		_, err := io.CopyN(rr.buf, rr.r, max(int64(len(p)), 2*rr.maxLen))
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return 0, err //nolint:wrapcheck
			}

			rr.isEOF = true
		}

		if err := rr.redactToBuffer(); err != nil {
			return 0, err
		}

		if rr.isEOF {
			if rr.buf.Len() > 0 {
				// Copy trailing data from buffer to output
				_, _ = io.Copy(rr.out, rr.buf)
			}
			if rr.out.Len() == 0 {
				// Nothing left to do
				return 0, io.EOF
			}
		}
	}
}

func (rr *redactReader) redactToBuffer() error { //nolint:cyclop
	if rr.isEOF && rr.buf.Len() == 0 {
		return nil
	}

	var err error

	// Find all matches
	var matches []matchInfo
	firstPartial := -1
	for _, pattern := range rr.matches {
		indexes, partial := byteslice.IndexAllPartial(rr.buf.Bytes(), pattern)
		for _, index := range indexes {
			matches = append(matches, matchInfo{start: index, end: index + len(pattern)})
		}
		if partial != -1 && (firstPartial == -1 || partial < firstPartial) {
			firstPartial = partial
		}
	}

	if len(matches) == 0 {
		if firstPartial == -1 {
			// no matches, no partial, copy it all
			_, err = io.Copy(rr.out, rr.buf)
		} else if firstPartial > 0 {
			// Leave partial match in buffer
			_, err = io.CopyN(rr.out, rr.buf, int64(firstPartial))
		}
		return err //nolint:wrapcheck
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
			_, err = io.CopyN(rr.out, rr.buf, int64(match.start-lastEnd))
		}

		// Redact the match
		rr.out.Write(rr.mask)

		// discard the match
		rr.buf.Next(match.end - match.start)

		lastEnd = match.end
	}

	if firstPartial != -1 {
		// Leave partial match in buffer
		if len(matches) > 0 && lastEnd < firstPartial {
			_, err = io.CopyN(rr.out, rr.buf, int64(firstPartial-lastEnd))
		}
	} else if rr.buf.Len() > 0 && firstPartial == -1 {
		// No partial match, copy all of the buffer to the output
		_, err = io.Copy(rr.out, rr.buf)
	}

	return err //nolint:wrapcheck
}
