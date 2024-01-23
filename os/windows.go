package os

import (
	"bufio"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"github.com/k0sproject/rig/exec"
	ps "github.com/k0sproject/rig/pkg/powershell"
)

// Windows is the base package for windows OS support
type Windows struct {
	exec.SimpleRunner
}

// Kind returns "windows"
func (c Windows) Kind() string {
	return "windows"
}

const privCheck = `"$currentPrincipal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent()); if (!$currentPrincipal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) { $host.SetShouldExit(1) }"`

// CheckPrivilege returns an error if the user does not have admin access to the host
func (c Windows) CheckPrivilege() error {
	if err := c.Exec(privCheck, exec.PS()); err != nil {
		return fmt.Errorf("%w: %w", exec.ErrSudo, err)
	}

	return nil
}

// InstallPackage enables an optional windows feature
func (c Windows) InstallPackage(s ...string) error {
	for _, n := range s {
		err := c.Exec("Enable-WindowsOptionalFeature -Online -FeatureName %s -All", n, exec.PS())
		if err != nil {
			return fmt.Errorf("failed to enable windows feature %s: %w", n, err)
		}
	}

	return nil
}

// InstallFile on windows is a regular file move operation
func (c Windows) InstallFile(src, dst, _ string) error {
	if err := c.Exec("move /y %s %s", ps.DoubleQuotePath(src), ps.DoubleQuotePath(dst)); err != nil {
		return fmt.Errorf("failed to move %s to %s: %w", src, dst, err)
	}
	return nil
}

// Pwd returns the current working directory
func (c Windows) Pwd() string {
	if pwd, err := c.ExecOutput("echo %%cd%%"); err == nil {
		return pwd
	}

	return ""
}

// JoinPath joins a path
func (c Windows) JoinPath(parts ...string) string {
	return strings.Join(parts, "\\")
}

// Hostname resolves the short hostname
func (c Windows) Hostname() string {
	output, err := c.ExecOutput("powershell.exe $env:COMPUTERNAME", exec.TrimOutput(true))
	if err != nil {
		return "localhost"
	}
	return output
}

// LongHostname resolves the FQDN (long) hostname
func (c Windows) LongHostname() string {
	output, err := c.ExecOutput("powershell.exe ([System.Net.Dns]::GetHostByName(($env:COMPUTERNAME))).Hostname", exec.TrimOutput(true))
	if err != nil {
		return "localhost.localdomain"
	}
	return output
}

// IsContainer returns true if the host is actually a container (always false on windows for now)
func (c Windows) IsContainer() bool {
	return false
}

// FixContainer makes a container work like a real host (does nothing on windows for now)
func (c Windows) FixContainer() error {
	return nil
}

// SELinuxEnabled is true when SELinux is enabled (always false on windows for now)
func (c Windows) SELinuxEnabled() bool {
	return false
}

// WriteFile writes file to host with given contents. Do not use for large files.
// The permissions argument is ignored on windows.
func (c Windows) WriteFile(path string, data string, _ string) error {
	err := c.Exec(`powershell.exe -Command "$Input | Out-File -FilePath %s"`, ps.DoubleQuotePath(path), exec.StdinString(data), exec.RedactString(data))
	if err != nil {
		return fmt.Errorf("failed to write to file %s: %w", path, err)
	}

	return nil
}

// ReadFile reads a file's contents from the host.
func (c Windows) ReadFile(path string) (string, error) {
	out, err := c.ExecOutput(`type %s`, ps.DoubleQuotePath(path), exec.HideOutput())
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", path, err)
	}
	return out, nil
}

// DeleteFile deletes a file from the host.
func (c Windows) DeleteFile(path string) error {
	if err := c.Exec(`del /f %s`, ps.DoubleQuotePath(path)); err != nil {
		return fmt.Errorf("failed to delete file %s: %w", path, err)
	}
	return nil
}

// FileExist checks if a file exists on the host
func (c Windows) FileExist(path string) bool {
	return c.Exec(`powershell.exe -Command "if (!(Test-Path -Path \"%s\")) { exit 1 }"`, ps.DoubleQuotePath(path)) == nil
}

// UpdateEnvironment updates the hosts's environment variables
func (c Windows) UpdateEnvironment(env map[string]string) error {
	for k, v := range env {
		err := c.Exec(`setx %s %s`, ps.DoubleQuote(k), ps.DoubleQuote(v))
		if err != nil {
			return fmt.Errorf("failed to set environment variable %s: %w", k, err)
		}
	}
	return nil
}

// UpdateServiceEnvironment does nothing on windows
func (c Windows) UpdateServiceEnvironment(_ string, _ map[string]string) error {
	return nil
}

// CleanupEnvironment removes environment variable configuration
func (c Windows) CleanupEnvironment(env map[string]string) error {
	for k := range env {
		err := c.Exec(`powershell.exe "[Environment]::SetEnvironmentVariable(%s, $null, 'User')"`, ps.SingleQuote(k))
		if err != nil {
			return fmt.Errorf("failed to remove user environment variable %s: %w", k, err)
		}
		err = c.Exec(`powershell "[Environment]::SetEnvironmentVariable(%s, $null, 'Machine')"`, ps.SingleQuote(k))
		if err != nil {
			return fmt.Errorf("failed to remove machine environment variable %s: %w", k, err)
		}
	}
	return nil
}

// CleanupServiceEnvironment does nothing on windows
func (c Windows) CleanupServiceEnvironment(_ string) error {
	return nil
}

// CommandExist returns true if the provided command exists
func (c Windows) CommandExist(cmd string) bool {
	return c.Exec("where /q %s", cmd) == nil
}

// Reboot executes the reboot command
func (c Windows) Reboot() error {
	if err := c.Exec("shutdown /r /t 5"); err != nil {
		return fmt.Errorf("failed to reboot: %w", err)
	}
	return nil
}

// MkDir creates a directory (including intermediate directories)
func (c Windows) MkDir(s string) error {
	// windows mkdir is "-p" by default
	if err := c.Exec(`mkdir %s`, ps.DoubleQuote(s)); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", s, err)
	}
	return nil
}

// Chmod on windows does nothing
func (c Windows) Chmod(_ string) error {
	return nil
}

// Stat gets file / directory information
func (c Windows) Stat(path string) (*FileInfo, error) {
	info := &FileInfo{FName: path, FMode: fs.FileMode(0)}

	out, err := c.ExecOutput("[System.Math]::Truncate((Get-Date -Date ((Get-Item -LiteralPath %s).LastWriteTime.ToUniversalTime()) -UFormat %%s))", ps.DoubleQuotePath(path), exec.PS())
	if err != nil {
		return nil, fmt.Errorf("failed to get file %s modtime: %w", path, err)
	}
	ts, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse file %s timestamp: %w", path, err)
	}
	info.FModTime = time.Unix(ts, 0)

	out, err = c.ExecOutput("(Get-Item -LiteralPath %s).Length", ps.DoubleQuotePath(path), exec.PS())
	if err != nil {
		return nil, fmt.Errorf("failed to get file %s size: %w", path, err)
	}
	size, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse file %s size: %w", path, err)
	}
	info.FSize = size

	out, err = c.ExecOutput("(Get-Item -LiteralPath %s).GetType().Name", ps.DoubleQuotePath(path), exec.PS())
	if err != nil {
		return nil, fmt.Errorf("failed to get file %s type: %w", path, err)
	}
	info.FIsDir = strings.Contains(out, "DirectoryInfo")

	return info, nil
}

// Touch updates a file's last modified time or creates a new empty file
func (c Windows) Touch(path string, ts time.Time) error {
	if !c.FileExist(path) {
		if err := c.Exec("Set-Content -LiteralPath %s -value $null", ps.DoubleQuotePath(path), exec.PS()); err != nil {
			return fmt.Errorf("failed to create file %s: %w", path, err)
		}
	}

	err := c.Exec("(Get-Item -LiteralPath %s).LastWriteTime = (Get-Date %s)", ps.DoubleQuotePath(path), ps.DoubleQuote(ts.Format(time.RFC3339)), exec.PS())
	if err != nil {
		return fmt.Errorf("failed to update file %s timestamp: %w", path, err)
	}
	return nil
}

// LineIntoFile tries to find a line starting with the matcher and replace it with a new entry. If match isn't found, the string is appended to the file.
// TODO this is a straight copypaste from linux, figure out a way to share these
func (c Windows) LineIntoFile(path, matcher, newLine string) error {
	newLine = strings.TrimSuffix(newLine, "\n")
	content, err := c.ReadFile(path)
	if err != nil {
		content = ""
	}

	var found bool
	writer := new(strings.Builder)

	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		row := scanner.Text()

		if strings.HasPrefix(row, matcher) && !found {
			row = newLine
			found = true
		}

		fmt.Fprintln(writer, row)
	}

	if !found {
		fmt.Fprintln(writer, newLine)
	}

	return c.WriteFile(path, writer.String(), "0644")
}

// Sha256sum returns the sha256sum of a file
func (c Windows) Sha256sum(path string) (string, error) {
	sum, err := c.ExecOutput("(Get-FileHash %s -Algorithm SHA256).Hash.ToLower()", ps.DoubleQuotePath(path), exec.PS())
	if err != nil {
		return "", fmt.Errorf("failed to get sha256sum for %s: %w", path, err)
	}
	return strings.TrimSpace(sum), nil
}
