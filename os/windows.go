package os

import (
	"encoding/json"

	"github.com/k0sproject/rig/exec"
	ps "github.com/k0sproject/rig/powershell"
)

type windowsVersion struct {
	Caption string `json:"Caption"`
	Version string `json:"Version"`
}

// ResolveWindows resolves the OS release information for a windows host.
func ResolveWindows(conn exec.SimpleRunner) *Release {
	if !conn.IsWindows() {
		return nil
	}
	script := ps.Cmd("Get-CimInstance -ClassName Win32_OperatingSystem | Select-Object Caption, Version | ConvertTo-Json")
	output, err := conn.ExecOutput(script)
	if err != nil {
		return nil
	}
	var winver windowsVersion
	if err := json.Unmarshal([]byte(output), &winver); err != nil {
		return nil
	}
	return &Release{
		ID:      "windows",
		IDLike:  "windows",
		Name:    winver.Caption,
		Version: winver.Version,
	}
}

// RegisterWindows registers the windows OS release resolver to a provider.
func RegisterWindows(provider *Provider) {
	provider.Register(ResolveWindows)
}
