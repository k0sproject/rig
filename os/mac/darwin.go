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

// InstallPackage installs a package using brew
func (c Darwin) InstallPackage(s ...string) error {
	cmd := strings.Builder{}
	cmd.WriteString("brew install")
	for _, pkg := range s {
		cmd.WriteRune(' ')
		cmd.WriteString(shellescape.Quote(pkg))
	}

	if err := c.Exec(cmd.String()); err != nil {
		return fmt.Errorf("failed to install packages %s: %w", s, err)
	}
	return nil
}

// Stat returns a FileInfo describing the named file
func (c Darwin) Stat(path string) (*os.FileInfo, error) {
	info := &os.FileInfo{FName: path}
	out, err := c.ExecOutput(`stat -f "%z/%m/%p/%HT" ` + shellescape.Quote(path))
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
func (c Darwin) Touch(path string, ts time.Time) error {
	if err := c.Linux.Touch(path, ts); err != nil {
		return fmt.Errorf("failed to touch %s: %w", path, err)
	}
	return nil
}

func init() {
	registry.RegisterOSModule(
		func(os rig.OSVersion) bool {
			return os.ID == "darwin"
		},
		func(runner exec.SimpleRunner) any {
			return &Darwin{Linux: os.Linux{SimpleRunner: runner}}
		},
	)
}
