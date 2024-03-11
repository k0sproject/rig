package iostream

import (
	"bytes"
	"fmt"
	"io"
	"testing"
)

type collector struct {
	tokens []string
}

func (c *collector) fn(token string) {
	c.tokens = append(c.tokens, token)
}

const lf byte = '\n'

func TestScanWriter(t *testing.T) {
	testCases := []struct {
		name        string
		buffer      []byte
		expectedOut []string
	}{
		{"no delimiter in input",
			[]byte("Hello, World"),
			[]string{
				"Hello, World",
			},
		},
		{"typical input",
			[]byte("Hello, World!\nHow are you today?\nNice to meet you.\n"),
			[]string{
				"Hello, World!",
				"How are you today?",
				"Nice to meet you.",
			},
		},
		{"typical input, no trailing newline",
			[]byte("Hello, World!\nHow are you today?\nNice to meet you."),
			[]string{
				"Hello, World!",
				"How are you today?",
				"Nice to meet you.",
			},
		},
	}

	for _, tc := range testCases {
		for _, bufSize := range []int{1, 2, 100} {
			t.Run(fmt.Sprintf("%s bufsize %d", tc.name, bufSize), func(t *testing.T) {
				col := &collector{}
				reader := bytes.NewReader(tc.buffer)
				buf := make([]byte, bufSize)
				sw := NewScanWriter(col.fn)
				for {
					n, err := reader.Read(buf)
					if err != nil && err != io.EOF {
						t.Fatalf("unexpected error: %v", err)
					}
					if n == 0 {
						sw.Close()
						break
					}
					wn, err := sw.Write(buf[:n])
					if wn != n {
						t.Fatalf("unexpected write count: %d != %d", wn, n)
					}
					if err != nil {
						t.Fatalf("unexpected error: %v", err)
					}
				}

				if len(col.tokens) != len(tc.expectedOut) {
					t.Fatalf("unexpected token count: %d != %d", len(col.tokens), len(tc.expectedOut))
				}

				for i, token := range col.tokens {
					if token != tc.expectedOut[i] {
						t.Fatalf("unexpected token: %s != %s", token, tc.expectedOut[i])
					}
				}
			})
		}
	}
}
