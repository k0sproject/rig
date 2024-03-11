// Package byteslice contains functions for working with byte slices.
package byteslice

import (
	"bytes"
)

// PartialIndex returns the index of the first occurrence of a partial match in the end of the buffer.
// If there is no partial match, -1 is returned.
func PartialIndex(buf, match []byte) int {
	if len(match) == 0 || len(buf) == 0 {
		return -1
	}

	for i := len(buf) - len(match) + 1; i < len(buf); i++ {
		if i < 0 {
			continue
		}
		if bytes.HasPrefix(match, buf[i:]) {
			return i
		}
	}

	return -1
}
