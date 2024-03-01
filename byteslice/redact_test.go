package byteslice

import (
	"bytes"
	"testing"
)

func TestRedactInPlace(t *testing.T) {
	testCases := []struct {
		name        string
		buf         []byte
		match       []byte
		expectedOut []byte
	}{
		{"match in the end", []byte("Hello, World"), []byte("World"), []byte("Hello, *****")},
		{"match in the start", []byte("Hello, World"), []byte("Hello"), []byte("*****, World")},
		{"multiple matches", []byte("Hello, World"), []byte("l"), []byte("He**o, Wor*d")},
		{"no matches", []byte("Hello, World"), []byte("ZZ"), []byte("Hello, World")},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			RedactInPlace(tc.buf, tc.match)
			if !bytes.Equal(tc.buf, tc.expectedOut) {
				t.Errorf("expected '%s', got '%s'", tc.expectedOut, tc.buf)
			}
		})
	}
}

func TestRedact(t *testing.T) {
	buf := []byte("Hello, World")
	match := []byte("l")
	expectedOut := "He**o, Wor*d"
	newBuf := Redact(buf, match)
	if string(newBuf) != expectedOut {
		t.Errorf("expected '%s', got '%s'", expectedOut, newBuf)
	}
	if string(buf) != "Hello, World" {
		t.Errorf("original slice was changed")
	}
}
