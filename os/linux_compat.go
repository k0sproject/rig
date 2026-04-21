package os

import (
	"context"
	"strings"

	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/log"
)

// packageManagerID maps a package manager binary name to a synthesized os.Release.
// Order matters: the slice is probed in sequence and the first hit wins.
var packageManagerID = []struct {
	bin     string
	id      string
	idLike  []string
	name    string
}{
	{"apk", "alpine", nil, "Alpine Linux"},
	{"pacman", "arch", nil, "Arch Linux"},
	{"emerge", "gentoo", nil, "Gentoo"},
	{"xbps-install", "void", nil, "Void Linux"},
	{"dnf", "fedora", []string{"rhel", "fedora"}, "Linux"},
	{"yum", "centos", []string{"rhel", "centos", "fedora"}, "Linux"},
	{"zypper", "opensuse-leap", []string{"suse", "opensuse"}, "Linux"},
	{"apt-get", "debian", nil, "Linux"},
}

// ResolveLinuxCompat is a fallback resolver for Linux hosts where /etc/os-release
// and /usr/lib/os-release are absent (distroless containers, minimal images, etc.).
// It probes for well-known package managers and synthesizes a *Release from the
// result. When confident (apk → Alpine, pacman → Arch, etc.) it sets ID directly;
// for family-based managers it sets IDLike so k0sctl's configurer fallback loop
// can still find a match. Name is set to "Linux (compatibility mode)" when the
// result is ambiguous, or the distro name when the package manager is unambiguous.
func ResolveLinuxCompat(conn cmd.SimpleRunner) (*Release, bool) {
	if conn.IsWindows() {
		return nil, false
	}

	if err := conn.Exec("uname | grep -q Linux"); err != nil {
		return nil, false
	}

	release := &Release{
		ID:   "linux",
		Name: "Linux (compatibility mode)",
	}

	if arch, err := conn.ExecOutput("uname -m"); err == nil {
		release.arch = strings.TrimSpace(arch)
	}

	for _, pm := range packageManagerID {
		if err := conn.Exec("command -v " + pm.bin + " > /dev/null 2>&1"); err == nil {
			release.ID = pm.id
			release.IDLike = pm.idLike
			if pm.idLike == nil {
				// unambiguous mapping: use the real distro name
				release.Name = pm.name
			} else {
				release.Name = pm.name + " (compatibility mode)"
			}
			log.Trace(context.Background(), "linux compat resolver: detected OS via package manager",
				log.HostAttr(conn),
				"package_manager", pm.bin,
				"id", release.ID,
			)
			return release, true
		}
	}

	log.Trace(context.Background(), "linux compat resolver: no package manager detected, using generic linux release",
		log.HostAttr(conn),
	)

	return release, true
}

// RegisterLinuxCompat registers the Linux compatibility resolver to a provider.
// It should be registered after ResolveLinux so it only activates when the
// standard os-release files are absent.
func RegisterLinuxCompat(provider *Registry) {
	provider.Register(ResolveLinuxCompat)
}
