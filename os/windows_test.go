package os

import (
	"testing"

	ps "github.com/k0sproject/rig/v2/powershell"
	"github.com/k0sproject/rig/v2/rigtest"
)

func TestResolveWindows(t *testing.T) {
	const cimOutput = `{"Caption":"Microsoft Windows Server 2022","Version":"10.0.20348"}`

	mr := rigtest.NewMockRunner()
	mr.Windows = true
	mr.AddCommandOutput(rigtest.Equal(ps.Cmd("Get-CimInstance -ClassName Win32_OperatingSystem | Select-Object Caption, Version | ConvertTo-Json")), cimOutput)
	mr.AddCommandOutput(rigtest.Equal(ps.Cmd("$env:PROCESSOR_ARCHITECTURE")), "AMD64")

	r, ok := ResolveWindows(mr)
	if !ok {
		t.Fatal("ResolveWindows returned false")
	}

	if r.ID != "windows" {
		t.Errorf("ID: got %q, want %q", r.ID, "windows")
	}
	if r.Name != "Microsoft Windows Server 2022" {
		t.Errorf("Name: got %q, want %q", r.Name, "Microsoft Windows Server 2022")
	}
	if r.Version != "10.0.20348" {
		t.Errorf("Version: got %q, want %q", r.Version, "10.0.20348")
	}

	arch, err := r.Arch()
	if err != nil {
		t.Errorf("Arch() unexpected error: %v", err)
	}
	if arch != "amd64" {
		t.Errorf("Arch(): got %q, want %q", arch, "amd64")
	}
}
