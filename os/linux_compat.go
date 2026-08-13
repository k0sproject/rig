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

// ResolveLinuxCompat is a fallback resolver for Linux hosts that os-release cannot
// name: /etc/os-release and /usr/lib/os-release are both absent (distroless
// containers, minimal images, etc.), unreadable, or do not carry an ID. It probes
// for well-known package managers and synthesizes a *Release from the result.
// Unambiguous mappings (apk → alpine, pacman → arch, etc.) set ID directly;
// family-based managers set IDLike only, leaving ID as "linux", so downstream
// configurers can still match via the IDLike fallback chain.
//
// It matches every such host, so a resolver of your own for one of them has to be
// registered ahead of it: before it in a registry you assemble yourself, or with
// Registry.RegisterFirst in one that already holds it.
func ResolveLinuxCompat(conn cmd.SimpleRunner) (*Release, bool) {
	if conn.IsWindows() {
		return nil, false
	}

	if err := conn.Exec("uname | grep -q Linux"); err != nil {
		return nil, false
	}

	// ResolveLinux identifies any host whose os-release names the distribution,
	// and this resolver matches every Linux host, so it has to stand down for
	// those rather than rely on being consulted afterwards. Deciding it from the
	// host keeps the two complementary wherever either sits in a registry. yum
	// (defers to dnf) and SysVinit (defers to systemd) exclude themselves the
	// same way.
	if _, ok := readOSRelease(conn); ok {
		log.Trace(context.Background(), "linux compat resolver: os-release identifies the host, deferring to the standard resolver",
			log.HostAttr(conn),
		)

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
// It excludes itself on any host ResolveLinux can identify, so the two can be
// registered in either order. It still matches every other Linux host, so a
// resolver that has to win against it belongs ahead of this call, or in
// Registry.RegisterFirst if the registry already holds it.
func RegisterLinuxCompat(provider *Registry) {
	provider.Register(ResolveLinuxCompat)
}
