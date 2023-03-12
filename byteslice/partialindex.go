package byteslice

import (
	"bytes"
)

// PartialIndex returns the index of the first occurrence of a partial match in the end of the buffer.
// If there is no partial match, -1 is returned.
func PartialIndex(buf, match []byte) int {
	// if either buffer is empty, there is no possibility of a match
	if len(match) == 0 || len(buf) == 0 {
		return -1
	}

	// if the match is longer than the buffer, there can only be a match if the buffer
	// is a prefix of the match
	if len(buf) < len(match) {
		if bytes.HasPrefix(match, buf) {
			return 0
		}
		return -1
	}

	// otherwise, the end of the buffer in the last match-1 bytes is the only place
	// where a partial match can occur
	for i := len(buf) - len(match) + 1; i < len(buf); i++ {
		if bytes.HasPrefix(match, buf[i:]) {
			return i
		}
	}

	return -1
}
