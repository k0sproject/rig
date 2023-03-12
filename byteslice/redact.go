package byteslice

import "bytes"

func RedactInPlace(buf []byte, match []byte) {
	matchLen := len(match)
	for i := 0; i <= len(buf)-matchLen; i++ {
		if bytes.HasPrefix(buf[i:], match) {
			for j := i; j < i+matchLen; j++ {
				buf[j] = '*'
			}
			i += matchLen - 1
		}
	}
}

func Redact(buf []byte, match []byte) []byte {
	out := make([]byte, len(buf))
	copy(out, buf)
	RedactInPlace(out, match)
	return out
}
