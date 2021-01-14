package rig

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"

	ps "github.com/k0sproject/rig/powershell"
)

func resolveLinux(c *Connection) (os OSVersion, err error) {
	output, err := c.ExecOutput("cat /etc/os-release || cat /usr/lib/os-release")
	if err != nil {
		return
	}

	err = parseOSReleaseFile(output, &os)

	return
}

func resolveWindows(c *Connection) (os OSVersion, err error) {
	osName, err := c.ExecOutput(ps.Cmd(`(Get-ItemProperty "HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion").ProductName`))
	if err != nil {
		return
	}

	osMajor, err := c.ExecOutput(ps.Cmd(`(Get-ItemProperty "HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion").CurrentMajorVersionNumber`))
	if err != nil {
		return
	}

	osMinor, err := c.ExecOutput(ps.Cmd(`(Get-ItemProperty "HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion").CurrentMinorVersionNumber`))
	if err != nil {
		return
	}

	osBuild, err := c.ExecOutput(ps.Cmd(`(Get-ItemProperty "HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion").CurrentBuild`))
	if err != nil {
		return
	}

	os = OSVersion{
		ID:      "windows",
		IDLike:  "windows",
		Version: fmt.Sprintf("%s.%s.%s", osMajor, osMinor, osBuild),
		Name:    osName,
	}

	return
}

func resolveDarwin(c *Connection) (os OSVersion, err error) {
	version, err := c.ExecOutput("sw_vers -productVersion")
	if err != nil {
		return
	}

	var name string
	if n, err := c.ExecOutput(`grep "SOFTWARE LICENSE AGREEMENT FOR " "/System/Library/CoreServices/Setup Assistant.app/Contents/Resources/en.lproj/OSXSoftwareLicense.rtf" | sed -E "s/^.*SOFTWARE LICENSE AGREEMENT FOR (.+)\\\/\1/"`); err == nil {
		name = fmt.Sprintf("%s %s", n, version)
	}

	os = OSVersion{
		ID:      "darwin",
		IDLike:  "darwin",
		Version: version,
		Name:    name,
	}

	return
}

func unquote(s string) string {
	if u, err := strconv.Unquote(s); err == nil {
		return u
	}
	return s
}

func parseOSReleaseFile(s string, os *OSVersion) error {
	scanner := bufio.NewScanner(strings.NewReader(s))
	for scanner.Scan() {
		fields := strings.SplitN(scanner.Text(), "=", 2)
		switch fields[0] {
		case "ID":
			os.ID = unquote(fields[1])
		case "ID_LIKE":
			os.IDLike = unquote(fields[1])
		case "VERSION_ID":
			os.Version = unquote(fields[1])
		case "PRETTY_NAME":
			os.Name = unquote(fields[1])
		}
	}

	return nil
}
