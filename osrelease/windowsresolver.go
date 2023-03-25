package osrelease

import (
	"encoding/json"
	"errors"

	"github.com/k0sproject/rig/powershell"
)

type WindowsResolver struct{}

type windowsVersion struct {
	Caption string `json:"Caption"`
	Version string `json:"Version"`
}

func (g *WindowsResolver) Get(r runner) (*OSRelease, error) {
	if !r.IsWindows() {
		return nil, errNoMatch
	}

	script := powershell.Cmd("Get-CimInstance -ClassName Win32_OperatingSystem | Select-Object Caption, Version | ConvertTo-Json")
	output, err := r.Run(script)
	if err != nil {
		return nil, errors.Join(errNoMatch, err)
	}

	var winver windowsVersion
	if err := json.Unmarshal([]byte(output), &winver); err != nil {
		return nil, errors.Join(errNoMatch, err)
	}

	return &OSRelease{
		ID:      "windows",
		IDLike:  "windows",
		Version: winver.Version,
		Name:    winver.Caption,
	}, nil
}
