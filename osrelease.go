package rig

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/k0sproject/rig/exec"
	ps "github.com/k0sproject/rig/powershell"
)

type resolveFunc func(exec.SimpleRunner) (OSRelease, error)

var (
	// Resolvers exposes an array of resolve functions where you can add your own if you need to detect some OS rig doesn't already know about
	// (consider making a PR)
	Resolvers = []resolveFunc{resolveLinux, resolveDarwin, resolveWindows}

	errAbort = errors.New("base os detected but version resolving failed")
)

type windowsVersion struct {
	Caption string `json:"Caption"`
	Version string `json:"Version"`
}

// OSRelease describes host operating system version information
type OSRelease struct {
	ID          string
	IDLike      string
	Name        string
	Version     string
	ExtraFields map[string]string
}

// String returns a human readable representation of OSRelease
func (o OSRelease) String() string {
	if o.Name != "" {
		return o.Name
	}
	return o.ID + " " + o.Version
}

// GetOSRelease runs through the Resolvers and tries to figure out the OS version information
func GetOSRelease(conn exec.SimpleRunner) (OSRelease, error) {
	for _, r := range Resolvers {
		os, err := r(conn)
		if err == nil {
			return os, nil
		}
		if errors.Is(err, errAbort) {
			return OSRelease{}, errors.Join(ErrNotSupported, err)
		}
	}
	return OSRelease{}, fmt.Errorf("%w: unable to determine host os", ErrNotSupported)
}

func resolveLinux(conn exec.SimpleRunner) (OSRelease, error) {
	if err := conn.Exec("uname | grep -q Linux"); err != nil {
		return OSRelease{}, fmt.Errorf("not a linux host (%w)", err)
	}

	output, err := conn.ExecOutput("cat /etc/os-release || cat /usr/lib/os-release")
	if err != nil {
		// at this point it is known that this is a linux host, so any error from here on should signal the resolver to not try the next
		return OSRelease{}, fmt.Errorf("%w: unable to read os-release file: %w", errAbort, err)
	}

	var version OSRelease
	version.ExtraFields = make(map[string]string)
	if err := parseOSReleaseFile(output, &version); err != nil {
		return OSRelease{}, errors.Join(errAbort, err)
	}
	return version, nil
}

func resolveWindows(conn exec.SimpleRunner) (OSRelease, error) {
	if !conn.IsWindows() {
		return OSRelease{}, fmt.Errorf("%w: not a windows host", ErrCommandFailed)
	}

	script := ps.Cmd("Get-CimInstance -ClassName Win32_OperatingSystem | Select-Object Caption, Version | ConvertTo-Json")
	output, err := conn.ExecOutput(script)
	if err != nil {
		return OSRelease{}, fmt.Errorf("%w: unable to get windows version: %w", errAbort, err)
	}
	var winver windowsVersion
	if err := json.Unmarshal([]byte(output), &winver); err != nil {
		return OSRelease{}, fmt.Errorf("%w: unable to parse windows version: %w", errAbort, err)
	}
	return OSRelease{
		ID:      "windows",
		IDLike:  "windows",
		Version: winver.Version,
		Name:    winver.Caption,
	}, nil
}

func resolveDarwin(conn exec.SimpleRunner) (OSRelease, error) {
	if err := conn.Exec("uname | grep -q Darwin"); err != nil {
		return OSRelease{}, fmt.Errorf("%w: not a darwin host: %w", ErrCommandFailed, err)
	}

	// at this point it is known that this is a windows host, so any error from here on should signal the resolver to not try the next
	version, err := conn.ExecOutput("sw_vers -productVersion")
	if err != nil {
		return OSRelease{}, fmt.Errorf("%w: unable to determine darwin version: %w", errAbort, err)
	}

	var name string
	if n, err := conn.ExecOutput(`grep "SOFTWARE LICENSE AGREEMENT FOR " "/System/Library/CoreServices/Setup Assistant.app/Contents/Resources/en.lproj/OSXSoftwareLicense.rtf" | sed -E "s/^.*SOFTWARE LICENSE AGREEMENT FOR (.+)\\\/\1/"`); err == nil {
		name = fmt.Sprintf("%s %s", n, version)
	}

	os := OSRelease{
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

func parseOSReleaseFile(s string, version *OSRelease) error {
	scanner := bufio.NewScanner(strings.NewReader(s))
	for scanner.Scan() {
		fields := strings.SplitN(scanner.Text(), "=", 2)
		switch fields[0] {
		case "":
			// Empty line in the file - unexpected but may happen
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
		return fmt.Errorf("%w: invalid or incomplete os-release file contents, at least ID and VERSION_ID required", ErrNotSupported)
	}

	return nil
}
