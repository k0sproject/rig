package os

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"github.com/alessio/shellescape"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/os/initsystem"
)

// Linux is a base module for various linux OS support packages
type Linux struct{}

// initSystem interface defines an init system - the OS's system to manage services (systemd, openrc for example)
type initSystem interface {
	StartService(initsystem.Host, string) error
	StopService(initsystem.Host, string) error
	RestartService(initsystem.Host, string) error
	DisableService(initsystem.Host, string) error
	EnableService(initsystem.Host, string) error
	ServiceIsRunning(initsystem.Host, string) bool
	ServiceScriptPath(initsystem.Host, string) (string, error)
	DaemonReload(initsystem.Host) error
	ServiceEnvironmentPath(initsystem.Host, string) (string, error)
	ServiceEnvironmentContent(map[string]string) string
}

// ErrInitSystemNotSupported is returned when the init system is not supported
var ErrInitSystemNotSupported = errors.New("init system not supported")

// Kind returns "linux"
func (c Linux) Kind() string {
	return "linux"
}

func (c Linux) hasSystemd(h Host) bool {
	return h.Exec("stat /run/systemd/system", exec.Sudo(h)) == nil
}

func (c Linux) hasUpstart(h Host) bool {
	return h.Exec(`stat /sbin/upstart-udev-bridge > /dev/null 2>&1 || \
    (stat /sbin/initctl > /dev/null 2>&1 && \
     /sbin/initctl --version 2> /dev/null | grep -q "\(upstart" )`, exec.Sudo(h)) == nil
}

func (c Linux) hasOpenRC(h Host) bool {
	return h.Exec(`command -v openrc-init > /dev/null 2>&1 || \
    (stat /etc/inittab > /dev/null 2>&1 && \
		  (grep ::sysinit: /etc/inittab | grep -q openrc) )`, exec.Sudo(h)) == nil
}

func (c Linux) hasSysV(h Host) bool {
	return h.Exec(`command -v service 2>&1 && stat /etc/init.d > /dev/null 2>&1`, exec.Sudo(h)) == nil
}

func (c Linux) is(h Host) (initSystem, error) {
	if c.hasSystemd(h) {
		return &initsystem.Systemd{}, nil
	}

	if c.hasOpenRC(h) || c.hasUpstart(h) || c.hasSysV(h) {
		return &initsystem.OpenRC{}, nil
	}

	return nil, fmt.Errorf("%w: failed to detect init system", ErrInitSystemNotSupported)
}

// StartService starts a service on the host
func (c Linux) StartService(h Host, s string) error {
	is, err := c.is(h)
	if err != nil {
		return err
	}
	if err := is.StartService(h, s); err != nil {
		return fmt.Errorf("failed to start service %s: %w", s, err)
	}
	return nil
}

// StopService stops a service on the host
func (c Linux) StopService(h Host, s string) error {
	is, err := c.is(h)
	if err != nil {
		return err
	}
	if err := is.StopService(h, s); err != nil {
		return fmt.Errorf("failed to stop service %s: %w", s, err)
	}
	return nil
}

// RestartService restarts a service on the host
func (c Linux) RestartService(h Host, s string) error {
	is, err := c.is(h)
	if err != nil {
		return err
	}
	if err := is.RestartService(h, s); err != nil {
		return fmt.Errorf("failed to restart service %s: %w", s, err)
	}
	return nil
}

// DisableService disables a service on the host
func (c Linux) DisableService(h Host, s string) error {
	is, err := c.is(h)
	if err != nil {
		return err
	}
	if err := is.DisableService(h, s); err != nil {
		return fmt.Errorf("failed to disable service %s: %w", s, err)
	}
	return nil
}

// EnableService enables a service on the host
func (c Linux) EnableService(h Host, s string) error {
	is, err := c.is(h)
	if err != nil {
		return err
	}
	if err := is.EnableService(h, s); err != nil {
		return fmt.Errorf("failed to enable service %s: %w", s, err)
	}
	return nil
}

// ServiceIsRunning returns true if the service is running on the host
func (c Linux) ServiceIsRunning(h Host, s string) bool {
	is, err := c.is(h)
	if err != nil {
		return false
	}
	return is.ServiceIsRunning(h, s)
}

// ServiceScriptPath returns the service definition file path on the host
func (c Linux) ServiceScriptPath(h Host, s string) (string, error) {
	is, err := c.is(h)
	if err != nil {
		return "", err
	}
	path, err := is.ServiceScriptPath(h, s)
	if err != nil {
		return "", fmt.Errorf("failed to get service script path for %s: %w", s, err)
	}
	return path, nil
}

// DaemonReload performs an init system config reload
func (c Linux) DaemonReload(h Host) error {
	is, err := c.is(h)
	if err != nil {
		return err
	}
	if err := is.DaemonReload(h); err != nil {
		return fmt.Errorf("failed to daemon-reload: %w", err)
	}
	return nil
}

// Pwd returns the current working directory of the session
func (c Linux) Pwd(h Host) string {
	pwd, err := h.ExecOutput("pwd 2> /dev/null")
	if err != nil {
		return ""
	}
	return pwd
}

// CheckPrivilege checks if the current user has root privileges
func (c Linux) CheckPrivilege(h Host) error {
	if err := h.Exec("true", exec.Sudo(h)); err != nil {
		return fmt.Errorf("%w: %w", exec.ErrSudo, err)
	}
	return nil
}

// JoinPath joins a path
func (c Linux) JoinPath(parts ...string) string {
	return strings.Join(parts, "/")
}

// Hostname resolves the short hostname
func (c Linux) Hostname(h Host) string {
	n, _ := h.ExecOutput("hostname 2> /dev/null")

	return n
}

// LongHostname resolves the FQDN (long) hostname
func (c Linux) LongHostname(h Host) string {
	n, _ := h.ExecOutput("hostname -f 2> /dev/null")

	return n
}

// IsContainer returns true if the host is actually a container
func (c Linux) IsContainer(h Host) bool {
	return h.Exec("grep 'container=docker' /proc/1/environ 2> /dev/null") == nil
}

// FixContainer makes a container work like a real host
func (c Linux) FixContainer(h Host) error {
	if err := h.Exec("mount --make-rshared / 2> /dev/null", exec.Sudo(h)); err != nil {
		return fmt.Errorf("failed to mount / as rshared: %w", err)
	}
	return nil
}

// SELinuxEnabled is true when SELinux is enabled
func (c Linux) SELinuxEnabled(h Host) bool {
	return h.Exec("getenforce | grep -iq enforcing 2> /dev/null", exec.Sudo(h)) == nil
}

// WriteFile writes file to host with given contents. Do not use for large files.
func (c Linux) WriteFile(h Host, path string, data string, permissions string) error {
	if data == "" {
		return fmt.Errorf("%w: empty content for write file %s", ErrCommandFailed, path)
	}

	if path == "" {
		return fmt.Errorf("%w: empty path for write file", ErrCommandFailed)
	}

	tempFile, err := h.ExecOutput("mktemp 2> /dev/null")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}

	if err := h.Execf(`cat > %s`, tempFile, exec.Stdin(data), exec.RedactString(data)); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	if err := c.InstallFile(h, tempFile, path, permissions); err != nil {
		_ = c.DeleteFile(h, tempFile)
		return fmt.Errorf("failed to move file into place: %w", err)
	}

	return nil
}

// InstallFile installs a file to the host
func (c Linux) InstallFile(h Host, src, dst, permissions string) error {
	if err := h.Execf("install -D -m %s -- %s %s", permissions, src, dst, exec.Sudo(h)); err != nil {
		return fmt.Errorf("failed to install file %s to %s: %w", src, dst, err)
	}
	return nil
}

// ReadFile reads a files contents from the host.
func (c Linux) ReadFile(h Host, path string) (string, error) {
	out, err := h.ExecOutputf("cat -- %s 2> /dev/null", shellescape.Quote(path), exec.HideOutput(), exec.Sudo(h))
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", path, err)
	}
	return out, nil
}

// DeleteFile deletes a file from the host.
func (c Linux) DeleteFile(h Host, path string) error {
	if err := h.Execf(`rm -f -- %s 2> /dev/null`, shellescape.Quote(path), exec.Sudo(h)); err != nil {
		return fmt.Errorf("failed to delete file %s: %w", path, err)
	}
	return nil
}

// FileExist checks if a file exists on the host
func (c Linux) FileExist(h Host, path string) bool {
	return h.Execf(`test -e %s 2> /dev/null`, shellescape.Quote(path), exec.Sudo(h)) == nil
}

// LineIntoFile tries to find a line starting with the matcher and replace it with a new entry. If match isn't found, the string is appended to the file.
// TODO add exec.Opts (requires modifying readfile and writefile signatures)
func (c Linux) LineIntoFile(h Host, path, matcher, newLine string) error {
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

// UpdateEnvironment updates the hosts's environment variables
func (c Linux) UpdateEnvironment(h Host, env map[string]string) error {
	for k, v := range env {
		err := c.LineIntoFile(h, "/etc/environment", fmt.Sprintf("%s=", k), fmt.Sprintf("%s=%s", k, v))
		if err != nil {
			return err
		}
	}

	// Update current session environment from the /etc/environment
	if err := h.Exec(`while read -r pair; do if [[ $pair == ?* && $pair != \#* ]]; then export "$pair" || exit 2; fi; done < /etc/environment`); err != nil {
		return fmt.Errorf("failed to update environment: %w", err)
	}
	return nil
}

// UpdateServiceEnvironment updates environment variables for a service
func (c Linux) UpdateServiceEnvironment(h Host, s string, env map[string]string) error {
	is, err := c.is(h)
	if err != nil {
		return err
	}
	fp, err := is.ServiceEnvironmentPath(h, s)
	if err != nil {
		return fmt.Errorf("failed to get service environment path: %w", err)
	}
	err = c.WriteFile(h, fp, is.ServiceEnvironmentContent(env), "0660")
	if err != nil {
		return fmt.Errorf("update service environment: %w", err)
	}

	return c.DaemonReload(h)
}

// CleanupEnvironment removes environment variable configuration
func (c Linux) CleanupEnvironment(h Host, env map[string]string) error {
	for k := range env {
		err := c.LineIntoFile(h, "/etc/environment", fmt.Sprintf("^%s=", k), "")
		if err != nil {
			return err
		}
	}
	// remove empty lines
	if err := h.Exec(`sed -i '/^$/d' /etc/environment`, exec.Sudo(h)); err != nil {
		return fmt.Errorf("failed to cleanup environment: %w", err)
	}
	return nil
}

// CleanupServiceEnvironment updates environment variables for a service
func (c Linux) CleanupServiceEnvironment(h Host, s string) error {
	is, err := c.is(h)
	if err != nil {
		return err
	}
	fp, err := is.ServiceEnvironmentPath(h, s)
	if err != nil {
		return fmt.Errorf("failed to get service environment path: %w", err)
	}
	return c.DeleteFile(h, fp)
}

// CommandExist returns true if the command exists
func (c Linux) CommandExist(h Host, cmd string) bool {
	return h.Execf(`command -v -- "%s" 2> /dev/null`, cmd, exec.Sudo(h)) == nil
}

// Reboot executes the reboot command
func (c Linux) Reboot(h Host) error {
	cmd, err := h.Sudo("shutdown --reboot 0 2> /dev/null")
	if err != nil {
		return fmt.Errorf("failed to get sudo command: %w", err)
	}
	if err := h.Execf("%s && exit", cmd); err != nil {
		return fmt.Errorf("failed to reboot: %w", err)
	}
	return nil
}

// MkDir creates a directory (including intermediate directories)
func (c Linux) MkDir(h Host, s string, opts ...exec.Option) error {
	if err := h.Exec(fmt.Sprintf("mkdir -p -- %s", shellescape.Quote(s)), opts...); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", s, err)
	}
	return nil
}

// Chmod updates permissions of a path
func (c Linux) Chmod(h Host, s, perm string, opts ...exec.Option) error {
	if err := h.Exec(fmt.Sprintf("chmod %s -- %s", perm, shellescape.Quote(s)), opts...); err != nil {
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
func (c Linux) Stat(h Host, path string, opts ...exec.Option) (*FileInfo, error) {
	cmd := `env -i LC_ALL=C stat -c '%s|%y|%a|%F' -- ` + shellescape.Quote(path)

	out, err := h.ExecOutput(cmd, opts...)
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
func (c Linux) Touch(h Host, path string, ts time.Time, opts ...exec.Option) error {
	utc := ts.UTC()

	// The BusyBox format will be accepted by both BusyBox and GNU stat.
	format := busyboxDateTimeLayout

	// Sub-second precision in timestamps is supported by GNU, but not by
	// BusyBox. If there is sub-second precision in the provided timestamp, try
	// to detect BusyBox touch and if it's not BusyBox go on with the
	// full-precision GNU format instead.
	if !utc.Equal(utc.Truncate(time.Second)) {
		out, err := h.ExecOutput("env -i LC_ALL=C TZ=UTC touch --help 2>&1")
		if err != nil || !strings.Contains(out, "BusyBox") {
			format = gnuCoreutilsDateTimeLayout
		}
	}

	cmd := fmt.Sprintf("env -i LC_ALL=C TZ=UTC touch -m -d %s -- %s",
		shellescape.Quote(utc.Format(format)),
		shellescape.Quote(path),
	)

	if err := h.Exec(cmd, opts...); err != nil {
		return fmt.Errorf("failed to touch %s: %w", path, err)
	}
	return nil
}
