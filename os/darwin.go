package osrelease

import (
	"fmt"

	"github.com/k0sproject/rig/exec"
)

// ResolveDarwin resolves the OS release information for a darwin host
func ResolveDarwin(conn exec.SimpleRunner) *Release {
	if conn.IsWindows() {
		return nil
	}

	if err := conn.Exec("uname | grep -q Darwin"); err != nil {
		return nil
	}

	version, err := conn.ExecOutput("sw_vers -productVersion")
	if err != nil {
		return nil
	}

	var name string
	if n, err := conn.ExecOutput(`grep "SOFTWARE LICENSE AGREEMENT FOR " "/System/Library/CoreServices/Setup Assistant.app/Contents/Resources/en.lproj/OSXSoftwareLicense.rtf" | sed -E "s/^.*SOFTWARE LICENSE AGREEMENT FOR (.+)\\\/\1/"`); err == nil {
		name = fmt.Sprintf("%s %s", n, version)
	}

	return &Release{
		ID:      "darwin",
		IDLike:  "darwin",
		Version: version,
		Name:    name,
	}
}

// RegisterDarwin registers the darwin OS release resolver to a provider
func RegisterDarwin(provider *Provider) {
	provider.Register(ResolveDarwin)
}
