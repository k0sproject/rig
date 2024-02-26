package iostream_test

import (
	"testing"

	"github.com/k0sproject/rig/iostream"
)

func TestFlaggingDiscardWriter(t *testing.T) {
	var wrote bool
	w := iostream.CallbackDiscard(func() { wrote = true })
	_, err := w.Write([]byte("\n  "))
	if err != nil {
		t.Error("Write() should not return an error when writing")
	}
	if wrote {
		t.Error("Callback shouldn't be called if only whitespace has been written")
	}
	_, err = w.Write([]byte("hello"))
	if err != nil {
		t.Error("Write() should not return an error when writing")
	}
	if !wrote {
		t.Error("Callback should have been called if non-whitespace has been written")
	}
	wrote = false
	_, err = w.Write([]byte("hello again"))
	if err != nil {
		t.Error("Write() should not return an error when writing")
	}
	if wrote {
		t.Error("Callback should only be called once")
	}
}
