package os

import (
	"bufio"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"github.com/k0sproject/rig/exec"
	ps "github.com/k0sproject/rig/powershell"
)

// Windows is the base package for windows OS support
type Windows struct {
	exec.SimpleRunner
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
