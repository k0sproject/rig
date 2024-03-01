package redact_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/k0sproject/rig/redact"
)

func TestRedactWriter(t *testing.T) {
	testCases := []struct {
		name        string
		buffer      []byte
		match       [][]byte
		mask        []byte
		expectedOut []byte
	}{
		{"no matches", []byte("Hello, World"), [][]byte{[]byte("ZZZ")}, []byte("*"), []byte("Hello, World")},
		{"single full match", []byte("Hello, World"), [][]byte{[]byte("ll")}, []byte("??"), []byte("He??o, World")},
		{"two full matches", []byte("Hello, World"), [][]byte{[]byte("l")}, []byte("."), []byte("He..o, Wor.d")},
		{"two full matches, long mask", []byte("Hello, World"), [][]byte{[]byte("l")}, []byte("REDACTED"), []byte("HeREDACTEDREDACTEDo, WorREDACTEDd")},
		{"non-completing partial match", []byte("Hello, World"), [][]byte{[]byte("World!")}, []byte("*"), []byte("Hello, World")},
		{"completing partial match", []byte("Hello, World!"), [][]byte{[]byte("World")}, []byte("[REDACTED]"), []byte("Hello, [REDACTED]!")},
		{"match is a subset of the mask", []byte("Hello, World!"), [][]byte{[]byte(",")}, []byte(",,"), []byte("Hello,, World!")},
		{"multiple matchers", []byte("Hello, World!"), [][]byte{[]byte("l"), []byte("o")}, []byte("*"), []byte("He***, W*r*d!")},
		{"multiple matchers, long mask, partial hit", []byte("Hello, World"), [][]byte{[]byte("lo"), []byte("World!")}, []byte("***"), []byte("Hel***, World")},
	}

	for _, tc := range testCases {
		for _, bufSize := range []int{1, 2, 5, 100, 1000} {
			t.Run(fmt.Sprintf("%s bufsize %d", tc.name, bufSize), func(t *testing.T) {
				out := &bytes.Buffer{}
				redactWriter := redact.Writer(out, tc.mask, tc.match...)
				n := 0
				for {
					chunk := make([]byte, min(bufSize, len(tc.buffer)-n))
					nread, err := bytes.NewReader(tc.buffer[n:]).Read(chunk)
					if err != nil {
						if errors.Is(err, io.EOF) {
							break
						}
						t.Fatalf("Error reading data: %v", err)
					}
					n += nread
					_, err = redactWriter.Write(chunk[:nread])
					if err != nil {
						t.Fatalf("Error writing data: %v", err)
					}
				}
				if err := redactWriter.Close(); err != nil {
					t.Fatalf("Error closing writer: %v", err)
				}
				outBytes := out.Bytes()
				if len(outBytes) != len(tc.expectedOut) {
					t.Errorf("Expected %d bytes, but got %d", len(tc.expectedOut), len(outBytes))
				}
				if !bytes.Equal(outBytes, tc.expectedOut) {
					t.Errorf("Output not what expected!\n\tTest parameters: %+v\n\tExpected %s, but got %s", tc, string(tc.expectedOut), string(outBytes))
				}
			})
		}
	}
}
