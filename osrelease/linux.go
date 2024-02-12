package osrelease

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"

	"github.com/k0sproject/rig/exec"
)

// ResolveLinux resolves the OS release information for a linux host
func ResolveLinux(conn exec.SimpleRunner) *OSRelease {
	if conn.IsWindows() {
		return nil
	}

	if err := conn.Exec("uname | grep -q Linux"); err != nil {
		return nil
	}

	output, err := conn.ExecOutput("cat /etc/os-release || cat /usr/lib/os-release")
	if err != nil {
		return nil
	}

	version := &OSRelease{}
	if err := parseOSReleaseFile(output, version); err != nil {
		return nil
	}
	return version
}

func parseOSReleaseFile(s string, version *OSRelease) error {
	scanner := bufio.NewScanner(strings.NewReader(s))
	for scanner.Scan() {
		fields := strings.SplitN(scanner.Text(), "=", 2)
		switch fields[0] {
		case "ID":
			version.ID = unquote(fields[1])
		case "ID_LIKE":
			version.IDLike = unquote(fields[1])
		case "VERSION_ID":
			version.Version = unquote(fields[1])
		case "PRETTY_NAME":
			version.Name = unquote(fields[1])
		default:
			if version.ExtraFields == nil {
				version.ExtraFields = make(map[string]string)
			}
			version.ExtraFields[fields[0]] = unquote(fields[1])
		}
	}

	// ArchLinux has no versions
	if version.ID == "arch" || version.IDLike == "arch" {
		version.Version = "0.0.0"
	}

	if version.ID == "" || version.Version == "" {
		return fmt.Errorf("%w: invalid or incomplete os-release file contents, at least ID and VERSION_ID required", ErrNotRecognized)
	}

	return nil
}

func unquote(s string) string {
	if u, err := strconv.Unquote(s); err == nil {
		return u
	}
	return s
}

// RegisterLinux registers the linux OS release resolver to a provider
func RegisterLinux(provider *Provider) {
	provider.Register(ResolveLinux)
}
