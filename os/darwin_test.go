package os

import (
	"errors"
	"testing"

	"github.com/k0sproject/rig/v2/rigtest"
)

func TestResolveDarwin(t *testing.T) {
	mr := rigtest.NewMockRunner()
	mr.AddCommand(rigtest.Equal("uname | grep -q Darwin"), func(_ *rigtest.A) error { return nil })
	mr.AddCommandOutput(rigtest.Equal("sw_vers -productVersion"), "14.5")
	mr.AddCommandOutput(rigtest.Equal("uname -m"), "arm64")
	mr.AddCommandFailure(rigtest.HasPrefix("grep"), errors.New("not found"))

	r, ok := ResolveDarwin(mr)
	if !ok {
		t.Fatal("ResolveDarwin returned false")
	}

	if r.ID != "darwin" {
		t.Errorf("ID: got %q, want %q", r.ID, "darwin")
	}
	if r.Version != "14.5" {
		t.Errorf("Version: got %q, want %q", r.Version, "14.5")
	}
	if r.Name != "" {
		t.Errorf("Name: got %q, want empty (name lookup failed)", r.Name)
	}

	arch, err := r.Arch()
	if err != nil {
		t.Errorf("Arch() unexpected error: %v", err)
	}
	if arch != "arm64" {
		t.Errorf("Arch(): got %q, want %q", arch, "arm64")
	}
}
