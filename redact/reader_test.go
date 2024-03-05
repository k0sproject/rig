package redact_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/k0sproject/rig/redact"
)

func TestRedactReader(t *testing.T) {
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
		{"nothing but match", []byte("Hello, World!"), []string{"Hello, World!"}, ".", []byte(".")},
	}

	for _, tc := range testCases {
		for _, bufSize := range []int{1, 2, 5, 100, 1000} {
			t.Run(fmt.Sprintf("%s bufsize %d", tc.name, bufSize), func(t *testing.T) {
				out := &bytes.Buffer{}
				buf := make([]byte, bufSize)
				redactReader := redact.Reader(bytes.NewReader(tc.buffer), tc.mask, tc.match...)
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
					t.Errorf("Output not what expected!\n\tTest parameters: %+v\n\tExpected %s, but got %s", tc, string(tc.expectedOut), string(outBytes))
				}
			})
		}
	}
}
