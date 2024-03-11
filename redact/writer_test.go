package redact_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/k0sproject/rig/v2/redact"
)

func TestRedactWriter(t *testing.T) {
	testCases := []struct {
		name        string
		buffer      []byte
		match       []string
		mask        string
		expectedOut []byte
	}{
		{"no matches", []byte("Hello, World"), []string{"ZZZ"}, "*", []byte("Hello, World")},
		{"single full match", []byte("Hello, World"), []string{"ll"}, "??", []byte("He??o, World")},
		{"two full matches", []byte("Hello, World"), []string{"l"}, ".", []byte("He..o, Wor.d")},
		{"two full matches, long mask", []byte("Hello, World"), []string{"l"}, "REDACTED", []byte("HeREDACTEDREDACTEDo, WorREDACTEDd")},
		{"non-completing partial match", []byte("Hello, World"), []string{"World!"}, "*", []byte("Hello, World")},
		{"completing partial match", []byte("Hello, World!"), []string{"World"}, "[REDACTED]", []byte("Hello, [REDACTED]!")},
		{"match is a subset of the mask", []byte("Hello, World!"), []string{","}, ",,", []byte("Hello,, World!")},
		{"multiple matchers", []byte("Hello, World!"), []string{"l", "o"}, "*", []byte("He***, W*r*d!")},
		{"multiple matchers, long mask, partial hit", []byte("Hello, World"), []string{"lo", "World!"}, "***", []byte("Hel***, World")},
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
