package os

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"github.com/k0sproject/rig/exec"
	ps "github.com/k0sproject/rig/pkg/powershell"
)

var errNotSupported = errors.New("not supported on windows")

// Windows is the base package for windows OS support
type Windows struct{}

// Kind returns "windows"
func (c Windows) Kind() string {
	return "windows"
}

const privCheck = `"$currentPrincipal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent()); if (!$currentPrincipal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) { $host.SetShouldExit(1) }"`

// CheckPrivilege returns an error if the user does not have admin access to the host
func (c Windows) CheckPrivilege(h Host) error {
	if err := h.Exec(ps.Cmd(privCheck)); err != nil {
		return fmt.Errorf("%w: %w", exec.ErrSudo, err)
	}

	return nil
}

// InstallPackage enables an optional windows feature
func (c Windows) InstallPackage(h Host, s ...string) error {
	for _, n := range s {
		err := h.Exec(ps.Cmd(fmt.Sprintf("Enable-WindowsOptionalFeature -Online -FeatureName %s -All", n)))
		if err != nil {
			return fmt.Errorf("failed to enable windows feature %s: %w", n, err)
		}
	}

	return nil
}

// InstallFile on windows is a regular file move operation
func (c Windows) InstallFile(h Host, src, dst, _ string) error {
	if err := h.Execf("move /y %s %s", ps.DoubleQuotePath(src), ps.DoubleQuotePath(dst), exec.Sudo(h)); err != nil {
		return fmt.Errorf("failed to move %s to %s: %w", src, dst, err)
	}
	return nil
}

// Pwd returns the current working directory
func (c Windows) Pwd(h Host) string {
	if pwd, err := h.ExecOutput("echo %cd%"); err == nil {
		return pwd
	}

	return ""
}

// JoinPath joins a path
func (c Windows) JoinPath(parts ...string) string {
	return strings.Join(parts, "\\")
}

// Hostname resolves the short hostname
func (c Windows) Hostname(h Host) string {
	output, err := h.ExecOutput("powershell $env:COMPUTERNAME")
	if err != nil {
		return "localhost"
	}

	return strings.TrimSpace(output)
}

// LongHostname resolves the FQDN (long) hostname
func (c Windows) LongHostname(h Host) string {
	output, err := h.ExecOutput("powershell ([System.Net.Dns]::GetHostByName(($env:COMPUTERNAME))).Hostname")
	if err != nil {
		return "localhost.localdomain"
	}
	return strings.TrimSpace(output)
}

// IsContainer returns true if the host is actually a container (always false on windows for now)
func (c Windows) IsContainer(_ Host) bool {
	return false
}

// FixContainer makes a container work like a real host (does nothing on windows for now)
func (c Windows) FixContainer(_ Host) error {
	return nil
}

// SELinuxEnabled is true when SELinux is enabled (always false on windows for now)
func (c Windows) SELinuxEnabled(_ Host) bool {
	return false
}

// WriteFile writes file to host with given contents. Do not use for large files.
// The permissions argument is ignored on windows.
func (c Windows) WriteFile(h Host, path string, data string, _ string) error {
	err := h.Exec(fmt.Sprintf(`powershell -Command "$Input | Out-File -FilePath %s"`, ps.DoubleQuotePath(path)), exec.Stdin(data), exec.RedactString(data))
	if err != nil {
		return fmt.Errorf("failed to write to file %s: %w", path, err)
	}

	return nil
}

// ReadFile reads a file's contents from the host.
func (c Windows) ReadFile(h Host, path string) (string, error) {
	out, err := h.ExecOutput(fmt.Sprintf(`type %s`, ps.DoubleQuotePath(path)), exec.HideOutput())
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", path, err)
	}
	return out, nil
}

// DeleteFile deletes a file from the host.
func (c Windows) DeleteFile(h Host, path string) error {
	if err := h.Exec(fmt.Sprintf(`del /f %s`, ps.DoubleQuotePath(path))); err != nil {
		return fmt.Errorf("failed to delete file %s: %w", path, err)
	}
	return nil
}

// FileExist checks if a file exists on the host
func (c Windows) FileExist(h Host, path string) bool {
	return h.Exec(fmt.Sprintf(`powershell -Command "if (!(Test-Path -Path \"%s\")) { exit 1 }"`, ps.DoubleQuotePath(path))) == nil
}

// UpdateEnvironment updates the hosts's environment variables
func (c Windows) UpdateEnvironment(h Host, env map[string]string) error {
	for k, v := range env {
		err := h.Exec(fmt.Sprintf(`setx %s %s`, ps.DoubleQuote(k), ps.DoubleQuote(v)))
		if err != nil {
			return fmt.Errorf("failed to set environment variable %s: %w", k, err)
		}
	}
	return nil
}

// UpdateServiceEnvironment does nothing on windows
func (c Windows) UpdateServiceEnvironment(_ Host, _ string, _ map[string]string) error {
	return nil
}

// CleanupEnvironment removes environment variable configuration
func (c Windows) CleanupEnvironment(h Host, env map[string]string) error {
	for k := range env {
		err := h.Exec(fmt.Sprintf(`powershell "[Environment]::SetEnvironmentVariable(%s, $null, 'User')"`, ps.SingleQuote(k)))
		if err != nil {
			return fmt.Errorf("failed to remove user environment variable %s: %w", k, err)
		}
		err = h.Exec(fmt.Sprintf(`powershell "[Environment]::SetEnvironmentVariable(%s, $null, 'Machine')"`, ps.SingleQuote(k)))
		if err != nil {
			return fmt.Errorf("failed to remove machine environment variable %s: %w", k, err)
		}
	}
	return nil
}

// CleanupServiceEnvironment does nothing on windows
func (c Windows) CleanupServiceEnvironment(_ Host, _ string) error {
	return nil
}

// CommandExist returns true if the provided command exists
func (c Windows) CommandExist(h Host, cmd string) bool {
	return h.Execf("where /q %s", cmd) == nil
}

// Reboot executes the reboot command
func (c Windows) Reboot(h Host) error {
	if err := h.Exec("shutdown /r /t 5"); err != nil {
		return fmt.Errorf("failed to reboot: %w", err)
	}
	return nil
}

// StartService starts a service
func (c Windows) StartService(h Host, s string) error {
	if err := h.Execf(`sc start %s`, ps.DoubleQuote(s)); err != nil {
		return fmt.Errorf("failed to start service %s: %w", s, err)
	}
	return nil
}

// StopService stops a service
func (c Windows) StopService(h Host, s string) error {
	if err := h.Execf(`sc stop %s`, ps.DoubleQuote(s)); err != nil {
		return fmt.Errorf("failed to stop service %s: %w", s, err)
	}
	return nil
}

// ServiceScriptPath returns the path to a service configuration file
func (c Windows) ServiceScriptPath(_ Host, _ string) (string, error) {
	return "", errNotSupported
}

// RestartService restarts a service
func (c Windows) RestartService(h Host, s string) error {
	if err := h.Execf(ps.Cmd(fmt.Sprintf(`Restart-Service %s`, ps.DoubleQuote(s)))); err != nil {
		return fmt.Errorf("failed to restart service %s: %w", s, err)
	}
	return nil
}

// DaemonReload reloads init system configuration. No-op on windows.
func (c Windows) DaemonReload(_ Host) error {
	return nil
}

// EnableService enables a service
func (c Windows) EnableService(h Host, s string) error {
	if err := h.Execf(`sc.exe config %s start=enabled`, ps.DoubleQuote(s)); err != nil {
		return fmt.Errorf("failed to enable service %s: %w", s, err)
	}

	return nil
}

// DisableService disables a service
func (c Windows) DisableService(h Host, s string) error {
	if err := h.Execf(`sc.exe config %s start=disabled`, ps.DoubleQuote(s)); err != nil {
		return fmt.Errorf("failed to disable service %s: %w", s, err)
	}
	return nil
}

// ServiceIsRunning returns true if a service is running
func (c Windows) ServiceIsRunning(h Host, s string) bool {
	return h.Execf(`sc.exe query %s | findstr "RUNNING"`, ps.DoubleQuote(s)) == nil
}

// MkDir creates a directory (including intermediate directories)
func (c Windows) MkDir(h Host, s string, opts ...exec.Option) error {
	// windows mkdir is "-p" by default
	if err := h.Exec(fmt.Sprintf(`mkdir %s`, ps.DoubleQuote(s)), opts...); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", s, err)
	}
	return nil
}

// Chmod on windows does nothing
func (c Windows) Chmod(_ Host, _, _ string, _ ...exec.Option) error {
	return nil
}

// Stat gets file / directory information
func (c Windows) Stat(h Host, path string, opts ...exec.Option) (*FileInfo, error) {
	info := &FileInfo{FName: path, FMode: fs.FileMode(0)}

	out, err := h.ExecOutput(ps.Cmd(fmt.Sprintf("[System.Math]::Truncate((Get-Date -Date ((Get-Item -LiteralPath %s).LastWriteTime.ToUniversalTime()) -UFormat %%s))", ps.DoubleQuotePath(path))), opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to get file %s modtime: %w", path, err)
	}
	ts, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse file %s timestamp: %w", path, err)
	}
	info.FModTime = time.Unix(ts, 0)

	out, err = h.ExecOutput(ps.Cmd(fmt.Sprintf("(Get-Item -LiteralPath %s).Length", ps.DoubleQuotePath(path))), opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to get file %s size: %w", path, err)
	}
	size, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse file %s size: %w", path, err)
	}
	info.FSize = size

	out, err = h.ExecOutput(ps.Cmd(fmt.Sprintf("(Get-Item -LiteralPath %s).GetType().Name", ps.DoubleQuotePath(path))), opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to get file %s type: %w", path, err)
	}
	info.FIsDir = strings.Contains(out, "DirectoryInfo")

	return info, nil
}

// Touch updates a file's last modified time or creates a new empty file
func (c Windows) Touch(h Host, path string, ts time.Time, opts ...exec.Option) error {
	if !c.FileExist(h, path) {
		if err := h.Exec(ps.Cmd(fmt.Sprintf("Set-Content -LiteralPath %s -value $null", ps.DoubleQuotePath(path))), opts...); err != nil {
			return fmt.Errorf("failed to create file %s: %w", path, err)
		}
	}

	err := h.Exec(ps.Cmd(fmt.Sprintf("(Get-Item -LiteralPath %s).LastWriteTime = (Get-Date %s)", ps.DoubleQuotePath(path), ps.DoubleQuote(ts.Format(time.RFC3339)))), opts...)
	if err != nil {
		return fmt.Errorf("failed to update file %s timestamp: %w", path, err)
	}
	return nil
}

// LineIntoFile tries to find a line starting with the matcher and replace it with a new entry. If match isn't found, the string is appended to the file.
// TODO this is a straight copypaste from linux, figure out a way to share these
func (c Windows) LineIntoFile(h Host, path, matcher, newLine string) error {
	newLine = strings.TrimSuffix(newLine, "\n")
	content, err := c.ReadFile(h, path)
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

	return c.WriteFile(h, path, writer.String(), "0644")
}

// Sha256sum returns the sha256sum of a file
func (c Windows) Sha256sum(h Host, path string, opts ...exec.Option) (string, error) {
	sum, err := h.ExecOutput(ps.Cmd(fmt.Sprintf("(Get-FileHash %s -Algorithm SHA256).Hash.ToLower()", ps.DoubleQuotePath(path))), opts...)
	if err != nil {
		return "", fmt.Errorf("failed to get sha256sum for %s: %w", path, err)
	}
	return strings.TrimSpace(sum), nil
}
