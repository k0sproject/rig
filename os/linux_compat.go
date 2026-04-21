package os

import (
	"context"
	"strings"

	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/log"
)

// packageManagerID maps a package manager binary name to a synthesized *Release.
// Order matters: the slice is probed in sequence and the first hit wins.
// Entries with a nil idLike are unambiguous (one package manager → one distro);
// entries with a non-nil idLike are family-based and only IDLike is set on the result.
var packageManagerID = []struct {
	bin    string
	id     string   // set on Release only when idLike is nil (unambiguous)
	idLike []string // non-nil means ambiguous family; ID stays "linux"
	name   string   // distro display name; empty for ambiguous entries
}{
	{"apk", "alpine", nil, "Alpine Linux"},
	{"pacman", "arch", nil, "Arch Linux"},
	{"emerge", "gentoo", nil, "Gentoo"},
	{"xbps-install", "void", nil, "Void Linux"},
	{"dnf", "", []string{"rhel", "fedora"}, ""},
	{"yum", "", []string{"rhel", "centos", "fedora"}, ""},
	{"zypper", "", []string{"suse", "opensuse"}, ""},
	{"apt-get", "", []string{"debian"}, ""},
}

// ResolveLinuxCompat is a fallback resolver for Linux hosts where /etc/os-release
// and /usr/lib/os-release are absent (distroless containers, minimal images, etc.).
// It probes for well-known package managers and synthesizes a *Release from the
// result. Unambiguous mappings (apk → alpine, pacman → arch, etc.) set ID directly;
// family-based managers set IDLike only, leaving ID as "linux", so downstream
// configurers can still match via the IDLike fallback chain.
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

	for _, entry := range packageManagerID {
		if err := conn.Exec("command -v " + entry.bin + " > /dev/null 2>&1"); err != nil {
			continue
		}
		if entry.idLike == nil {
			release.ID = entry.id
			release.Name = entry.name
		} else {
			release.IDLike = entry.idLike
		}
		log.Trace(context.Background(), "linux compat resolver: detected OS via package manager",
			log.HostAttr(conn),
			"package_manager", entry.bin,
			"id", release.ID,
			"id_like", release.IDLike,
		)
		return release, true
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
