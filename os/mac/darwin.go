// Package darwin provides a configurer for macOS
package darwin

import (
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"github.com/alessio/shellescape"
	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/os/registry"
)

// ErrNotImplemented is returned when a method is not implemented
var ErrNotImplemented = errors.New("not implemented")

// Darwin provides OS support for macOS Darwin
type Darwin struct {
	os.Linux
}

// Kind returns "darwin"
func (c Darwin) Kind() string {
	return "darwin"
}

// StartService starts a service
func (c Darwin) StartService(h os.Host, s string) error {
	if err := h.Execf(`launchctl start %s`, s); err != nil {
		return fmt.Errorf("failed to start service %s: %w", s, err)
	}
	return nil
}

// StopService stops a service
func (c Darwin) StopService(h os.Host, s string) error {
	if err := h.Execf(`launchctl stop %s`, s); err != nil {
		return fmt.Errorf("failed to stop service %s: %w", s, err)
	}
	return nil
}

// ServiceScriptPath returns the path to a service configuration file
func (c Darwin) ServiceScriptPath(s string) (string, error) {
	return "", fmt.Errorf("%w: service scripts are not supported on darwin", ErrNotImplemented)
}

// RestartService restarts a service
func (c Darwin) RestartService(h os.Host, s string) error {
	if err := h.Execf(`launchctl kickstart -k %s`, s); err != nil {
		return fmt.Errorf("failed to restart service %s: %w", s, err)
	}
	return nil
}

// DaemonReload reloads init system configuration -- does nothing on darwin
func (c Darwin) DaemonReload(_ os.Host) error {
	return nil
}

// EnableService enables a service
func (c Darwin) EnableService(h os.Host, s string) error {
	if err := h.Execf(`launchctl enable %s`, s); err != nil {
		return fmt.Errorf("failed to enable service %s: %w", s, err)
	}
	return nil
}

// DisableService disables a service
func (c Darwin) DisableService(h os.Host, s string) error {
	if err := h.Execf(`launchctl disable %s`, s); err != nil {
		return fmt.Errorf("failed to disable service %s: %w", s, err)
	}
	return nil
}

// ServiceIsRunning returns true if a service is running
func (c Darwin) ServiceIsRunning(h os.Host, s string) bool {
	return h.Execf(`launchctl list %s | grep -q '"PID"'`, s) == nil
}

// InstallPackage installs a package using brew
func (c Darwin) InstallPackage(h os.Host, s ...string) error {
	if err := h.Execf("brew install %s", strings.Join(s, " ")); err != nil {
		return fmt.Errorf("failed to install packages %s: %w", s, err)
	}
	return nil
}

// Stat returns a FileInfo describing the named file
func (c Darwin) Stat(h os.Host, path string, opts ...exec.Option) (*os.FileInfo, error) {
	info := &os.FileInfo{FName: path}
	out, err := h.ExecOutput(`stat -f "%z/%m/%p/%HT" `+shellescape.Quote(path), opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to stat %s: %w", path, err)
	}
	fields := strings.SplitN(out, "/", 4)
	size, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse size %s: %w", fields[0], err)
	}
	info.FSize = size
	modtime, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse modtime %s: %w", fields[1], err)
	}
	info.FModTime = time.Unix(modtime, 0)
	mode, err := strconv.ParseUint(fields[2], 8, 32)
	if err != nil {
		return nil, fmt.Errorf("failed to parse mode %s: %w", fields[2], err)
	}
	info.FMode = fs.FileMode(mode)
	info.FIsDir = strings.Contains(fields[3], "directory")

	return info, nil
}

// Touch creates a file if it doesn't exist, or updates the modification time if it does
func (c Darwin) Touch(h os.Host, path string, ts time.Time, opts ...exec.Option) error {
	if err := c.Linux.Touch(h, path, ts, opts...); err != nil {
		return fmt.Errorf("failed to touch %s: %w", path, err)
	}
	return nil
}

func init() {
	registry.RegisterOSModule(
		func(os rig.OSVersion) bool {
			return os.ID == "darwin"
		},
		func() interface{} {
			return Darwin{}
		},
	)
}
