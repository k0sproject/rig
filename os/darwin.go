package os

import (
	"fmt"
	"strings"

	"github.com/k0sproject/rig/v2/cmd"
)

// ResolveDarwin resolves the OS release information for a darwin host.
func ResolveDarwin(conn cmd.SimpleRunner) (*Release, bool) {
	if conn.IsWindows() {
		return nil, false
	}

	if err := conn.Exec("uname | grep -q Darwin"); err != nil {
		return nil, false
	}

	version, err := conn.ExecOutput("sw_vers -productVersion")
	if err != nil {
		return nil, false
	}

	var name string
	if n, err := conn.ExecOutput(`grep "SOFTWARE LICENSE AGREEMENT FOR " "/System/Library/CoreServices/Setup Assistant.app/Contents/Resources/en.lproj/OSXSoftwareLicense.rtf" | sed -E "s/^.*SOFTWARE LICENSE AGREEMENT FOR (.+)\\\/\1/"`); err == nil {
		name = fmt.Sprintf("%s %s", n, version)
	}

	release := &Release{
		ID:      "darwin",
		Version: version,
		Name:    name,
	}

	if arch, err := conn.ExecOutput("uname -m"); err == nil {
		release.arch = strings.TrimSpace(arch)
	}

	return release, true
}

// RegisterDarwin registers the darwin OS release resolver to a provider.
func RegisterDarwin(provider *Registry) {
	provider.Register(ResolveDarwin)
}
