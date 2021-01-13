package rig

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"

	ps "github.com/k0sproject/rig/powershell"
)

// Resolver interface
type Resolver interface {
	Resolve(*Connection) (Os, error)
}

// GetResolver returns an OS version resolver
func GetResolver(c *Connection) (Resolver, error) {
	isWin, err := c.IsWindows()
	if err != nil {
		return nil, err
	}
	if isWin {
		return WindowsResolver{}, nil
	}

	if err := c.Exec("uname | grep -q Darwin"); err == nil {
		return DarwinResolver{}, nil
	}

	return LinuxResolver{}, nil
}

// LinuxResolver resolves linux versions
type LinuxResolver struct{}

// WindowsResolver resolves windows versions
type WindowsResolver struct{}

// DarwinResolver resolves mac versions
type DarwinResolver struct{}

// Resolve resolves OS release information
func (w LinuxResolver) Resolve(c *Connection) (os Os, err error) {
	output, err := c.ExecWithOutput("cat /etc/os-release || cat /usr/lib/os-release")
	if err != nil {
		return
	}

	err = parseOSReleaseFile(output, &os)

	return
}

func parseOSReleaseFile(s string, os *Os) error {
	scanner := bufio.NewScanner(strings.NewReader(s))
	for scanner.Scan() {
		fields := strings.SplitN(scanner.Text(), "=", 2)
		switch fields[0] {
		case "ID":
			id, err := strconv.Unquote(fields[1])
			if err != nil {
				return err
			}
			os.ID = id
		case "ID_LIKE":
			idlike, err := strconv.Unquote(fields[1])
			if err != nil {
				return err
			}
			os.IDLike = idlike
		case "VERSION_ID":
			version, err := strconv.Unquote(fields[1])
			if err != nil {
				return err
			}
			os.Version = version
		case "PRETTY_NAME":
			name, err := strconv.Unquote(fields[1])
			if err != nil {
				return err
			}
			os.Name = name
		}
	}

	return nil
}

// Resolve resolves OS release information
func (w WindowsResolver) Resolve(c *Connection) (os Os, err error) {
	osName, err := c.ExecWithOutput(ps.Cmd(`(Get-ItemProperty "HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion").ProductName`))
	if err != nil {
		return
	}

	osMajor, err := c.ExecWithOutput(ps.Cmd(`(Get-ItemProperty "HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion").CurrentMajorVersionNumber`))
	if err != nil {
		return
	}

	osMinor, err := c.ExecWithOutput(ps.Cmd(`(Get-ItemProperty "HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion").CurrentMinorVersionNumber`))
	if err != nil {
		return
	}

	osBuild, err := c.ExecWithOutput(ps.Cmd(`(Get-ItemProperty "HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion").CurrentBuild`))
	if err != nil {
		return
	}

	os = Os{
		ID:      "windows",
		IDLike:  "windows",
		Version: fmt.Sprintf("%s.%s.%s", osMajor, osMinor, osBuild),
		Name:    osName,
	}

	return
}

// Resolve resolves OS release information
func (w DarwinResolver) Resolve(c *Connection) (os Os, err error) {
	version, err := c.ExecWithOutput("sw_vers -productVersion")
	if err != nil {
		return
	}

	var name string
	if n, err := c.ExecWithOutput(`grep "SOFTWARE LICENSE AGREEMENT FOR " "/System/Library/CoreServices/Setup Assistant.app/Contents/Resources/en.lproj/OSXSoftwareLicense.rtf" | sed -E "s/^.*SOFTWARE LICENSE AGREEMENT FOR (.+)\\\/\1/"`); err != nil {
		name = fmt.Sprintf("%s %s", n, version)
	}

	os = Os{
		ID:      "darwin",
		IDLike:  "darwin",
		Version: version,
		Name:    name,
	}

	return
}
