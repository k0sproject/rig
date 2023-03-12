package byteslice

import (
	"bytes"
	"testing"
)

func TestPartialIndex(t *testing.T) {
	testCases := []struct {
		name        string
		buf         []byte
		match       []byte
		expectedIdx int
	}{
		{"full match in the end", []byte("Hello, World"), []byte("World"), -1},
		{"partial match", []byte("Hello, World"), []byte("World!"), 7},
		{"short partial match", []byte("Hello, World"), []byte("d!"), 11},
		{"full match in the beginning", []byte("Hello, World"), []byte("Hello"), -1},
		{"full match in the middle", []byte("Hello, World"), []byte("ll"), -1},
		{"no match", []byte("Hello, World"), []byte("ZZZ"), -1},
		{"empty data", []byte{}, []byte("ZZZ"), -1},
		{"empty match", []byte("Hello, World"), []byte{}, -1},
		{"empty data and match", []byte{}, []byte{}, -1},
		{"match longer than buf", []byte("Hello, World"), []byte("Hello, World!"), 0},
		{"match longer than buf, no hit", []byte("Hello, World"), []byte("Aloha, World!"), -1},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actualIdx := PartialIndex(tc.buf, tc.match)

			if actualIdx != tc.expectedIdx {
				t.Errorf("Failed for buf: '%s', match: '%s'. Expected %d, but got %d",
					tc.buf, tc.match, tc.expectedIdx, actualIdx)
			}
		})
	}
}

func FuzzPartialIndex(f *testing.F) {
	f.Fuzz(func(t *testing.T, buf, match []byte) {
		idx := PartialIndex(buf, match)

		if len(buf) == 0 && idx != -1 {
			t.Errorf("Expected -1 for empty data but got %d", idx)
		}

		if len(match) == 0 && idx != -1 {
			t.Errorf("Expected -1 for empty match but got %d", idx)
		}

		if idx != -1 {
			if !bytes.Equal(buf[idx:], match[:len(buf[idx:])]) {
				t.Errorf("Expected partial match to be '%s', but it was '%s'", match[:len(buf[idx:])], buf[idx:])
			}
		}
	})
}
