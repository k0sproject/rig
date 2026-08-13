package os

import (
	"context"
	"strings"

	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/kv"
	"github.com/k0sproject/rig/v2/log"
)

// osReleaseCommand reads the os-release file from either of the two standard
// locations.
const osReleaseCommand = "cat /etc/os-release || cat /usr/lib/os-release"

// ResolveLinux resolves the OS release information for a linux host.
func ResolveLinux(conn cmd.SimpleRunner) (*Release, bool) {
	if conn.IsWindows() {
		return nil, false
	}

	if err := conn.Exec("uname | grep -q Linux"); err != nil {
		log.Trace(context.Background(), "linux os resolver: host is not linux", log.HostAttr(conn), log.ErrorAttr(err))
		return nil, false
	}

	release, ok := readOSRelease(conn)
	if !ok {
		return nil, false
	}

	if arch, err := conn.ExecOutput("uname -m"); err == nil {
		release.arch = strings.TrimSpace(arch)
	}

	return release, true
}

// readOSRelease parses the os-release file of a host already known to be Linux.
//
// It reports false unless the file yields an ID, since a Release that does not
// name the distribution is of no use to a caller. A host whose os-release is
// missing, unreadable or silent about the ID is left to ResolveLinuxCompat, which
// can still identify it from its package manager.
//
// ResolveLinuxCompat calls this to decide whether ResolveLinux is going to handle
// a host, which keeps the two resolvers complementary without either of them
// depending on the order they were registered in.
func readOSRelease(conn cmd.SimpleRunner) (*Release, bool) {
	release := &Release{}
	if err := kv.NewDecoder(conn.ExecReader(osReleaseCommand)).Decode(release); err != nil {
		log.Trace(context.Background(), "linux os resolver: failed to decode os-release", log.HostAttr(conn), log.ErrorAttr(err))
		return nil, false
	}

	if release.ID == "" {
		log.Trace(context.Background(), "linux os resolver: os-release did not yield an ID", log.HostAttr(conn))
		return nil, false
	}

	return release, true
}

// RegisterLinux registers the linux OS release resolver to a provider.
func RegisterLinux(provider *Registry) {
	provider.Register(ResolveLinux)
}
