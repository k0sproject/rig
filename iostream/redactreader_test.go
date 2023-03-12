package iostream

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"testing"
)

func TestRedactReader(t *testing.T) {
	testCases := []struct {
		name        string
		buffer      []byte
		match       []byte
		mask        []byte
		expectedOut []byte
	}{
		{"no matches", []byte("Hello, World"), []byte("ZZZ"), []byte("*"), []byte("Hello, World")},
		{"single full match", []byte("Hello, World"), []byte("ll"), []byte("??"), []byte("He??o, World")},
		{"two full matches", []byte("Hello, World"), []byte("l"), []byte("."), []byte("He..o, Wor.d")},
		{"two full matches, long mask", []byte("Hello, World"), []byte("l"), []byte("REDACTED"), []byte("HeREDACTEDREDACTEDo, WorREDACTEDd")},
		{"non-completing partial match", []byte("Hello, World"), []byte("World!"), []byte("*"), []byte("Hello, World")},
		{"completing partial match", []byte("Hello, World!"), []byte("World"), []byte("[REDACTED]"), []byte("Hello, [REDACTED]!")},
	}

	for _, tc := range testCases {
		for _, bufSize := range []int{1, 2, 100, 1000} {
			t.Run(fmt.Sprintf("%s bufsize %d", tc.name, bufSize), func(t *testing.T) {
				out := &bytes.Buffer{}
				buf := make([]byte, bufSize)
				redactReader := NewRedactReader(bytes.NewReader(tc.buffer), tc.match, tc.mask)
				for {
					n, err := redactReader.Read(buf)
					if n > bufSize {
						t.Fatalf("Read more bytes than the buffer size")
					}
					if n > 0 {
						_, err = out.Write(buf[:n])
						if err != nil {
							t.Fatalf("unexpected error while copying buffers: %v", err)
						}
					}
					if err != nil {
						if errors.Is(err, io.EOF) {
							break
						}
						t.Fatalf("unexpected error: %v", err)
					}
				}
				outBytes := out.Bytes()
				if len(outBytes) != len(tc.expectedOut) {
					t.Errorf("Expected %d bytes, but got %d", len(tc.expectedOut), len(outBytes))
				}
				if !bytes.Equal(outBytes, tc.expectedOut) {
					t.Errorf("Output not what expected!\n\tTest parameters: %+v\n\tExpected %s, but got %s", tc, tc.expectedOut, outBytes)
				}
			})
		}
	}
}
