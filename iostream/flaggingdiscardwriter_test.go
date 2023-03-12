package iostream

import "testing"

func TestFlaggingDiscardWriter(t *testing.T) {
	w := &FlaggingDiscardWriter{}
	if w.Written() {
		t.Error("Written() should be false on a new writer")
	}
	n, err := w.Write([]byte("\n"))
	if n != 1 {
		t.Error("Write() should return 1 when writing a newline")
	}
	if err != nil {
		t.Error("Write() should not return an error when writing")
	}
	if w.Written() {
		t.Error("Written() should be false if only whitespace has been written")
	}
	n, err = w.Write([]byte("hello"))
	if n != 5 {
		t.Error("Write() should return 5 when writing 'hello'")
	}
	if err != nil {
		t.Error("Write() should not return an error when writing")
	}
	if !w.Written() {
		t.Error("Written() should be true if non-whitespace has been written")
	}
}

func TestFlaggingDiscardWriterFunc(t *testing.T) {
	var written bool
	w := &FlaggingDiscardWriter{Fn: func() { written = true }}
	_, _ = w.Write([]byte("\n"))
	if written {
		t.Error("Callback shouldn't be called if only whitespace has been written")
	}
	_, _ = w.Write([]byte("hello"))
	if !written {
		t.Error("Callback should have been called if non-whitespace has been written")
	}
}
