package rig

import (
	"bufio"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/k0sproject/rig/errstring"
	"github.com/k0sproject/rig/log"
	ps "github.com/k0sproject/rig/powershell"
)

type resolveFunc func(*Connection) (OSVersion, error)

var (
	// Resolvers exposes an array of resolve functions where you can add your own if you need to detect some OS rig doesn't already know about
	// (consider making a PR)
	Resolvers = []resolveFunc{resolveLinux, resolveDarwin, resolveWindows}

	errAbort = errstring.New("base os detected, version resolving failed")
)

// GetOSVersion runs through the Resolvers and tries to figure out the OS version information
func GetOSVersion(conn *Connection) (OSVersion, error) {
	for _, r := range Resolvers {
		os, err := r(conn)
		if err == nil {
			return os, nil
		}
		if errors.Is(err, errAbort) {
			return OSVersion{}, ErrNotSupported.Wrap(err)
		}
		log.Tracef("resolver failed: %v", err)
	}
	return OSVersion{}, ErrNotSupported.Wrapf("unable to determine host os")
}

func resolveLinux(conn *Connection) (OSVersion, error) {
	if err := conn.Exec("uname | grep -q Linux"); err != nil {
		return OSVersion{}, ErrCommandFailed.Wrapf("not a linux host: %w", err)
	}

	output, err := conn.ExecOutput("cat /etc/os-release || cat /usr/lib/os-release")
	if err != nil {
		// at this point it is known that this is a linux host, so any error from here on should signal the resolver to not try the next
		return OSVersion{}, errAbort.Wrapf("unable to read os-release file: %w", err)
	}

	var version OSVersion
	if err := parseOSReleaseFile(output, &version); err != nil {
		return OSVersion{}, errAbort.Wrap(err)
	}
	return version, nil
}

func resolveWindows(conn *Connection) (OSVersion, error) {
	if !conn.IsWindows() {
		return OSVersion{}, ErrCommandFailed.Wrapf("not a windows host")
	}

	// at this point it is known that this is a windows host, so any error from here on should signal the resolver to not try the next
	osName, err := conn.ExecOutput(ps.Cmd(`(Get-ItemProperty "HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion").ProductName`))
	if err != nil {
		return OSVersion{}, errAbort.Wrapf("unable to determine windows product name: %w", err)
	}

	osMajor, err := conn.ExecOutput(ps.Cmd(`(Get-ItemProperty "HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion").CurrentMajorVersionNumber`))
	if err != nil {
		return OSVersion{}, errAbort.Wrapf("unable to determine windows major version: %w", err)
	}

	osMinor, err := conn.ExecOutput(ps.Cmd(`(Get-ItemProperty "HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion").CurrentMinorVersionNumber`))
	if err != nil {
		return OSVersion{}, errAbort.Wrapf("unable to determine windows minor version: %w", err)
	}

	osBuild, err := conn.ExecOutput(ps.Cmd(`(Get-ItemProperty "HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion").CurrentBuild`))
	if err != nil {
		return OSVersion{}, errAbort.Wrapf("unable to determine windows build version: %w", err)
	}

	version := OSVersion{
		ID:      "windows",
		IDLike:  "windows",
		Version: fmt.Sprintf("%s.%s.%s", osMajor, osMinor, osBuild),
		Name:    osName,
	}

	return version, nil
}

func resolveDarwin(conn *Connection) (OSVersion, error) {
	if err := conn.Exec("uname | grep -q Darwin"); err != nil {
		return OSVersion{}, ErrCommandFailed.Wrapf("not a darwin host: %w", err)
	}

	// at this point it is known that this is a windows host, so any error from here on should signal the resolver to not try the next
	version, err := conn.ExecOutput("sw_vers -productVersion")
	if err != nil {
		return OSVersion{}, errAbort.Wrapf("unable to determine darwin version: %w", err)
	}

	var name string
	if n, err := conn.ExecOutput(`grep "SOFTWARE LICENSE AGREEMENT FOR " "/System/Library/CoreServices/Setup Assistant.app/Contents/Resources/en.lproj/OSXSoftwareLicense.rtf" | sed -E "s/^.*SOFTWARE LICENSE AGREEMENT FOR (.+)\\\/\1/"`); err == nil {
		name = fmt.Sprintf("%s %s", n, version)
	}

	os := OSVersion{
		ID:      "darwin",
		IDLike:  "darwin",
		Version: version,
		Name:    name,
	}

	return os, nil
}

func unquote(s string) string {
	if u, err := strconv.Unquote(s); err == nil {
		return u
	}
	return s
}

func parseOSReleaseFile(s string, version *OSVersion) error {
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
		}
	}

	// ArchLinux has no versions
	if version.ID == "arch" || version.IDLike == "arch" {
		version.Version = "0.0.0"
	}

	if version.ID == "" || version.Version == "" {
		return ErrNotSupported.Wrapf("invalid or incomplete os-release file contents, at least ID and VERSION_ID required")
	}

	return nil
}
