package darwin

import (
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

// Darwin provides OS support for macOS Darwin
type Darwin struct {
	os.Linux
}

// Kind returns "darwin"
func (c Darwin) Kind() string {
	return "darwin"
}

// StartService starts a a service
func (c Darwin) StartService(h os.Host, s string) error {
	return h.Execf(`launchctl start %s`, s)
}

// StopService stops a a service
func (c Darwin) StopService(h os.Host, s string) error {
	return h.Execf(`launchctl stop %s`, s)
}

// ServiceScriptPath returns the path to a service configuration file
func (c Darwin) ServiceScriptPath(s string) (string, error) {
	return "", fmt.Errorf("not available on mac")
}

// RestartService restarts a a service
func (c Darwin) RestartService(h os.Host, s string) error {
	return h.Execf(`launchctl kickstart -k %s`, s)
}

// DaemonReload reloads init system configuration
func (c Darwin) DaemonReload(_ os.Host) error {
	return nil
}

// EnableService enables a a service
func (c Darwin) EnableService(h os.Host, s string) error {
	return h.Execf(`launchctl enable %s`, s)
}

// DisableService disables a a service
func (c Darwin) DisableService(h os.Host, s string) error {
	return h.Execf(`launchctl disable %s`, s)
}

// ServiceIsRunning returns true if a service is running
func (c Darwin) ServiceIsRunning(h os.Host, s string) bool {
	return h.Execf(`launchctl list %s | grep -q '"PID"'`, s) == nil
}

// InstallPackage installs a package using brew
func (c Darwin) InstallPackage(h os.Host, s ...string) error {
	return h.Execf("brew install %s", strings.Join(s, " "))
}

func (c Darwin) Stat(h os.Host, path string, opts ...exec.Option) (*os.FileInfo, error) {
	f := &os.FileInfo{FName: path}
	out, err := h.ExecOutput(`stat -f "%z/%m/%p/%HT" `+shellescape.Quote(path), opts...)
	if err != nil {
		return nil, err
	}
	fields := strings.SplitN(out, "/", 4)
	size, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return nil, err
	}
	f.FSize = size
	modtime, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return nil, err
	}
	f.FModTime = time.Unix(modtime, 0)
	mode, err := strconv.ParseUint(fields[2], 8, 32)
	if err != nil {
		return nil, err
	}
	f.FMode = fs.FileMode(mode)
	f.FIsDir = strings.Contains(fields[3], "directory")

	return f, nil
}

func (c Darwin) Touch(h os.Host, path string, ts time.Time, opts ...exec.Option) error {
	return c.Linux.Touch(h, path, ts, opts...)
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
