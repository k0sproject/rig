package os

import (
	"bufio"
	"fmt"
	"io/fs"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/alessio/shellescape"
	"github.com/k0sproject/rig/exec"
)

// Linux is a base module for various linux OS support packages
type Linux struct {
	exec.SimpleRunner
}

// Kind returns "linux"
func (c Linux) Kind() string {
	return "linux"
}

// Pwd returns the current working directory of the session
func (c Linux) Pwd() string {
	pwd, err := c.ExecOutput("pwd 2> /dev/null")
	if err != nil {
		return ""
	}
	return pwd
}

// CheckPrivilege checks if the current user has root privileges
func (c Linux) CheckPrivilege() error {
	if err := c.Exec("true"); err != nil {
		return fmt.Errorf("%w: %w", exec.ErrSudo, err)
	}
	return nil
}

// JoinPath joins a path
func (c Linux) JoinPath(parts ...string) string {
	return path.Join(parts...)
}

// Hostname resolves the short hostname
func (c Linux) Hostname() string {
	n, err := c.ExecOutput("hostname 2> /dev/null")
	if err != nil {
		return ""
	}

	return n
}

// LongHostname resolves the FQDN (long) hostname
func (c Linux) LongHostname() string {
	n, _ := c.ExecOutput("hostname -f 2> /dev/null")

	return n
}

// IsContainer returns true if the host is actually a container
func (c Linux) IsContainer() bool {
	return c.Exec("grep 'container=docker' /proc/1/environ 2> /dev/null") == nil
}

// FixContainer makes a container work like a real host
func (c Linux) FixContainer() error {
	if err := c.Exec("mount --make-rshared / 2> /dev/null"); err != nil {
		return fmt.Errorf("failed to mount / as rshared: %w", err)
	}
	return nil
}

// SELinuxEnabled is true when SELinux is enabled
func (c Linux) SELinuxEnabled() bool {
	return c.Exec("getenforce | grep -iq enforcing 2> /dev/null") == nil
}

// WriteFile writes file to host with given contents. Do not use for large files.
func (c Linux) WriteFile(path string, data string, permissions string) error {
	if data == "" {
		return fmt.Errorf("%w: empty content for write file %s", ErrCommandFailed, path)
	}

	if path == "" {
		return fmt.Errorf("%w: empty path for write file", ErrCommandFailed)
	}

	tempFile, err := c.ExecOutput("mktemp 2> /dev/null")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}

	if err := c.Exec(`cat > %s`, shellescape.Quote(tempFile), exec.StdinString(data), exec.RedactString(data)); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	if err := c.InstallFile(tempFile, path, permissions); err != nil {
		_ = c.DeleteFile(tempFile)
		return fmt.Errorf("failed to move file into place: %w", err)
	}

	return nil
}

// InstallFile installs a file to the host
func (c Linux) InstallFile(src, dst, permissions string) error {
	if err := c.Exec("install -D -m %s -- %s %s", permissions, src, dst); err != nil {
		return fmt.Errorf("failed to install file %s to %s: %w", src, dst, err)
	}
	return nil
}

// ReadFile reads a files contents from the host.
func (c Linux) ReadFile(path string) (string, error) {
	out, err := c.ExecOutput("cat -- %s 2> /dev/null", shellescape.Quote(path), exec.TrimOutput(false))
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", path, err)
	}
	return out, nil
}

// DeleteFile deletes a file from the host.
func (c Linux) DeleteFile(path string) error {
	if err := c.Exec(`rm -f -- %s 2> /dev/null`, shellescape.Quote(path)); err != nil {
		return fmt.Errorf("failed to delete file %s: %w", path, err)
	}
	return nil
}

// FileExist checks if a file exists on the host
func (c Linux) FileExist(path string) bool {
	return c.Exec(`test -e %s 2> /dev/null`, shellescape.Quote(path)) == nil
}

// LineIntoFile tries to find a line starting with the matcher and replace it with a new entry. If match isn't found, the string is appended to the file.
// TODO add exec.Opts (requires modifying readfile and writefile signatures)
func (c Linux) LineIntoFile(path, matcher, newLine string) error {
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

// UpdateEnvironment updates the hosts's environment variables
func (c Linux) UpdateEnvironment(env map[string]string) error {
	for k, v := range env {
		err := c.LineIntoFile("/etc/environment", fmt.Sprintf("%s=", k), fmt.Sprintf("%s=%s", k, v))
		if err != nil {
			return err
		}
	}

	// Update current session environment from the /etc/environment
	if err := c.Exec(`while read -r pair; do if [[ $pair == ?* && $pair != \#* ]]; then export "$pair" || exit 2; fi; done < /etc/environment`); err != nil {
		return fmt.Errorf("failed to update environment: %w", err)
	}
	return nil
}

// CleanupEnvironment removes environment variable configuration
func (c Linux) CleanupEnvironment(env map[string]string) error {
	for k := range env {
		err := c.LineIntoFile("/etc/environment", fmt.Sprintf("^%s=", k), "")
		if err != nil {
			return err
		}
	}
	// remove empty lines
	if err := c.Exec(`sed -i '/^$/d' /etc/environment`); err != nil {
		return fmt.Errorf("failed to cleanup environment: %w", err)
	}
	return nil
}

// CommandExist returns true if the command exists
func (c Linux) CommandExist(cmd string) bool {
	return c.Exec(`command -v -- "%s" 2> /dev/null`, cmd) == nil
}

// Reboot executes the reboot command
func (c Linux) Reboot() error {
	cmd := "shutdown --reboot 0 2> /dev/null"
	if decorator, ok := c.SimpleRunner.(exec.CommandFormatter); ok {
		cmd = decorator.Command(cmd)
	}
	if err := c.Exec("%s && exit", cmd); err != nil {
		return fmt.Errorf("failed to reboot: %w", err)
	}
	return nil
}

// MkDir creates a directory (including intermediate directories)
func (c Linux) MkDir(s string) error {
	if err := c.Exec("mkdir -p -- %s", shellescape.Quote(s)); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", s, err)
	}
	return nil
}

// Chmod updates permissions of a path
func (c Linux) Chmod(s, perm string) error {
	if err := c.Exec("chmod %s -- %s", perm, shellescape.Quote(s)); err != nil {
		return fmt.Errorf("failed to chmod %s: %w", s, err)
	}
	return nil
}

// gnuCoreutilsDateTimeLayout represents the date and time format employed by GNU
// coreutils. Note that this is different from BSD coreutils.
const gnuCoreutilsDateTimeLayout = "2006-01-02 15:04:05.999999999 -0700"

// busyboxDateTimeLayout represents the date and time format that can be passed
// to BusyBox. BusyBox will happily output the GNU format, but will fail to
// parse it. Currently, there doesn't seem to be a way to support sub-second
// precision.
const busyboxDateTimeLayout = "2006-01-02 15:04:05"

// Stat gets file / directory information
func (c Linux) Stat(path string) (*FileInfo, error) {
	cmd := `env -i LC_ALL=C stat -c '%%s|%%y|%%a|%%F' -- ` + shellescape.Quote(path)

	out, err := c.ExecOutput(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to stat %s: %w", path, err)
	}

	fields := strings.SplitN(out, "|", 4)
	if len(fields) != 4 {
		err = fmt.Errorf("failed to stat %s: unrecognized output: %s", path, out) //nolint:goerr113
		return nil, err
	}

	size, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse file %s size: %w", path, err)
	}

	modTime, err := time.Parse(gnuCoreutilsDateTimeLayout, fields[1])
	if err != nil {
		return nil, fmt.Errorf("failed to parse file %s mod time: %w", path, err)
	}

	mode, err := strconv.ParseUint(fields[2], 8, 32)
	if err != nil {
		return nil, fmt.Errorf("failed to parse file %s mode: %w", path, err)
	}

	return &FileInfo{
		FName:    path,
		FSize:    size,
		FModTime: modTime,
		FMode:    fs.FileMode(mode),
		FIsDir:   strings.Contains(fields[3], "directory"),
	}, nil
}

// Touch updates a file's last modified time. It creates a new empty file if it
// didn't exist prior to the call to Touch.
func (c Linux) Touch(path string, ts time.Time) error {
	utc := ts.UTC()

	// The BusyBox format will be accepted by both BusyBox and GNU stat.
	format := busyboxDateTimeLayout

	// Sub-second precision in timestamps is supported by GNU, but not by
	// BusyBox. If there is sub-second precision in the provided timestamp, try
	// to detect BusyBox touch and if it's not BusyBox go on with the
	// full-precision GNU format instead.
	if !utc.Equal(utc.Truncate(time.Second)) {
		out, err := c.ExecOutput("env -i LC_ALL=C TZ=UTC touch --help 2>&1", exec.HideOutput(), exec.HideCommand())
		if err != nil || !strings.Contains(out, "BusyBox") {
			format = gnuCoreutilsDateTimeLayout
		}
	}

	cmd := fmt.Sprintf("env -i LC_ALL=C TZ=UTC touch -m -d %s -- %s",
		shellescape.Quote(utc.Format(format)),
		shellescape.Quote(path),
	)

	if err := c.Exec(cmd); err != nil {
		return fmt.Errorf("failed to touch %s: %w", path, err)
	}
	return nil
}

// Sha256sum calculates the sha256 checksum of a file
func (c Linux) Sha256sum(path string) (string, error) {
	out, err := c.ExecOutput("sha256sum -b -- %s 2> /dev/null", shellescape.Quote(path))
	if err != nil {
		return "", fmt.Errorf("failed to shasum %s: %w", path, err)
	}
	return strings.Split(out, " ")[0], nil
}
