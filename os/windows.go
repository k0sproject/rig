package os

import (
	"encoding/json"

	"github.com/k0sproject/rig/v2/cmd"
	ps "github.com/k0sproject/rig/v2/powershell"
)

type windowsVersion struct {
	Caption string `json:"Caption"`
	Version string `json:"Version"`
}

// ResolveWindows resolves the OS release information for a windows host.
func ResolveWindows(conn cmd.SimpleRunner) (*Release, bool) {
	if !conn.IsWindows() {
		return nil, false
	}
	script := ps.Cmd("Get-CimInstance -ClassName Win32_OperatingSystem | Select-Object Caption, Version | ConvertTo-Json")
	output, err := conn.ExecOutput(script)
	if err != nil {
		return nil, false
	}
	var winver windowsVersion
	if err := json.Unmarshal([]byte(output), &winver); err != nil {
		return nil, false
	}
	return &Release{
		ID:      "windows",
		Name:    winver.Caption,
		Version: winver.Version,
	}, true
}

// RegisterWindows registers the windows OS release resolver to a provider.
func RegisterWindows(provider *Provider) {
	provider.Register(ResolveWindows)
}
