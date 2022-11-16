package rig

import (
	"bufio"
	"errors"
	"fmt"
	"strconv"
	"strings"

	ps "github.com/k0sproject/rig/powershell"
)

var (
	// ErrInvalidOSRelease is returned when the OS release file is invalid
	ErrInvalidOSRelease = errors.New("invalid or incomplete os-release file contents, at least ID and VERSION_ID required")

	// ErrUnableToDetermineOS is returned when the OS cannot be determined
	ErrUnableToDetermineOS = errors.New("unable to determine host os")

	// ErrNotLinux is returned when the host is not Linux
	ErrNotLinux = errors.New("not a linux host")

	// ErrNotDarwin is returned when the host is not a darwin host
	ErrNotDarwin = errors.New("not a darwin host")
)

type resolveFunc func(*Connection) (OSVersion, error)

// Resolvers exposes an array of resolve functions where you can add your own if you need to detect some OS rig doesn't already know about
// (consider making a PR)
var Resolvers = []resolveFunc{resolveLinux, resolveWindows, resolveDarwin}

// GetOSVersion runs through the Resolvers and tries to figure out the OS version information
func GetOSVersion(conn *Connection) (OSVersion, error) {
	for _, r := range Resolvers {
		if os, err := r(conn); err == nil {
			return os, nil
		}
	}
	return OSVersion{}, ErrUnableToDetermineOS
}

func resolveLinux(conn *Connection) (OSVersion, error) {
	if err := conn.Exec("uname | grep -q Linux"); err != nil {
		return OSVersion{}, ErrNotLinux
	}

	output, err := conn.ExecOutput("cat /etc/os-release || cat /usr/lib/os-release")
	if err != nil {
		return OSVersion{}, fmt.Errorf("unable to read os-release file: %w", err)
	}

	var version OSVersion
	if err := parseOSReleaseFile(output, &version); err != nil {
		return OSVersion{}, err
	}
	return version, nil
}

func resolveWindows(conn *Connection) (OSVersion, error) {
	osName, err := conn.ExecOutput(ps.Cmd(`(Get-ItemProperty "HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion").ProductName`))
	if err != nil {
		return OSVersion{}, fmt.Errorf("unable to determine windows product name: %w", err)
	}

	osMajor, err := conn.ExecOutput(ps.Cmd(`(Get-ItemProperty "HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion").CurrentMajorVersionNumber`))
	if err != nil {
		return OSVersion{}, fmt.Errorf("unable to determine windows major version: %w", err)
	}

	osMinor, err := conn.ExecOutput(ps.Cmd(`(Get-ItemProperty "HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion").CurrentMinorVersionNumber`))
	if err != nil {
		return OSVersion{}, fmt.Errorf("unable to determine windows minor version: %w", err)
	}

	osBuild, err := conn.ExecOutput(ps.Cmd(`(Get-ItemProperty "HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion").CurrentBuild`))
	if err != nil {
		return OSVersion{}, fmt.Errorf("unable to determine windows build version: %w", err)
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
		return OSVersion{}, ErrNotDarwin
	}

	version, err := conn.ExecOutput("sw_vers -productVersion")
	if err != nil {
		return OSVersion{}, fmt.Errorf("unable to determine darwin version: %w", err)
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
		return ErrInvalidOSRelease
	}

	return nil
}
