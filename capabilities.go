package rig

import (
	"context"
	"errors"
	"fmt"

	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/os"
	"github.com/k0sproject/rig/v2/protocol"
)

var (
	errNilOS             = errors.New("os release provider returned nil")
	errNilFS             = errors.New("remote filesystem provider returned nil")
	errNilPackageManager = errors.New("package manager provider returned nil")
	errNilInitSystem     = errors.New("init system provider returned nil")
	errNilSudoRunner     = errors.New("sudo runner provider returned nil")
)

// Capabilities summarizes what was detected about the remote host.
// All detection results are memoized: the first call to Capabilities (or to any
// of the individual accessor methods such as OS, FS, PackageManager, etc.) that
// triggers a probe runs a remote command; every subsequent call returns the cached
// result without additional network round-trips.
type Capabilities struct {
	// Protocol is the connection protocol implementation name (e.g. "SSH", "OpenSSH", "WinRM", "Local").
	// OpenSSH and native SSH connections return distinct values ("OpenSSH" vs "SSH").
	// Free: derived from the connection without a remote round-trip.
	Protocol string

	// OS is the detected operating system release information.
	// Populated on the first call that resolves OS info; nil if detection failed.
	OS *os.Release
	// OSErr is the OS detection error, if any.
	OSErr error

	// RemoteFS indicates whether the remote filesystem is available via [Client.FS].
	RemoteFS bool
	// RemoteFSErr is the remote filesystem initialization error, if any.
	RemoteFSErr error

	// PackageManager is the name of the detected package manager (e.g. "apt", "yum",
	// "dnf"). Empty if no package manager was detected.
	PackageManager string
	// PackageManagerErr is the package manager detection error, if any.
	PackageManagerErr error

	// InitSystem is the name of the detected init system (e.g. "systemd", "openrc").
	// Empty if no init system was detected.
	InitSystem string
	// InitSystemErr is the init system detection error, if any.
	InitSystemErr error

	// Sudo indicates whether privileged command execution is available via [Client.Sudo].
	// This is true both when already running as root/administrator (no escalation needed)
	// and when a supported escalation mechanism (sudo, doas) was detected. False only when
	// [Client.Sudo] would return an error (e.g. no sudo binary found and not root).
	Sudo bool
	// SudoErr is the sudo detection error, if any.
	SudoErr error

	// InteractiveExec indicates whether the underlying connection supports interactive
	// command execution via [Client.ExecInteractive].
	// Free: determined by a local interface check with no remote round-trip.
	InteractiveExec bool
}

// sudoAvailable returns (true, nil) when runner is non-nil and err is nil,
// or (false, err/errNilSudoRunner) otherwise.
func sudoAvailable(runner cmd.Runner, err error) (bool, error) {
	switch {
	case err != nil:
		return false, err
	case runner == nil:
		return false, errNilSudoRunner
	default:
		return true, nil
	}
}

// String returns a human-readable summary of the detected capabilities.
func (c Capabilities) String() string {
	return fmt.Sprintf(
		"protocol=%s os=%q fs=%s package-manager=%s init-system=%s sudo=%s interactive-exec=%s",
		c.Protocol, c.osString(), c.fsString(), c.pmString(), c.isString(), c.sudoString(), c.interactiveExecString(),
	)
}

func (c Capabilities) osString() string {
	if c.OS != nil {
		return c.OS.String()
	}
	if c.OSErr != nil {
		return fmt.Sprintf("error: %v", c.OSErr)
	}
	return "unknown"
}

func (c Capabilities) fsString() string {
	if c.RemoteFS {
		return "available"
	}
	if c.RemoteFSErr != nil {
		return fmt.Sprintf("%q", "error: "+c.RemoteFSErr.Error())
	}
	return "unknown"
}

func (c Capabilities) pmString() string {
	if c.PackageManager != "" {
		return c.PackageManager
	}
	if c.PackageManagerErr != nil {
		return fmt.Sprintf("%q", "error: "+c.PackageManagerErr.Error())
	}
	return "none"
}

func (c Capabilities) isString() string {
	if c.InitSystem != "" {
		return c.InitSystem
	}
	if c.InitSystemErr != nil {
		return fmt.Sprintf("%q", "error: "+c.InitSystemErr.Error())
	}
	return "none"
}

func (c Capabilities) sudoString() string {
	if c.Sudo {
		return "yes"
	}
	if c.SudoErr != nil {
		return fmt.Sprintf("%q", "error: "+c.SudoErr.Error())
	}
	return "no"
}

func (c Capabilities) interactiveExecString() string {
	if c.InteractiveExec {
		return "yes"
	}
	return "no"
}

// Capabilities probes all detectable aspects of the remote host and returns a
// summary. Aspects that have already been probed (for example by a prior call to
// [Client.OS], [Client.FS], [Client.PackageManager], or [Client.Sudo]) return their
// cached result without issuing additional remote commands.
//
// Some capabilities may run remote commands on first access (OS, PackageManager,
// InitSystem, and Sudo); others are resolved locally without network round-trips
// (Protocol, RemoteFS, and InteractiveExec). All results are memoized so repeated
// calls or interleaved direct accessor calls remain cheap.
func (c *Client) Capabilities() Capabilities {
	caps := Capabilities{
		Protocol: c.ProtocolName(),
	}

	caps.OS, caps.OSErr = c.OSRelease()
	if caps.OS == nil && caps.OSErr == nil {
		caps.OSErr = errNilOS
	}

	if fs, err := c.RemoteFSProvider.FS(); err != nil {
		caps.RemoteFSErr = err
	} else if fs == nil {
		caps.RemoteFSErr = errNilFS
	} else {
		caps.RemoteFS = true
	}

	if pm, err := c.PackageManagerProvider.PackageManager(); err != nil {
		caps.PackageManagerErr = err
	} else if pm == nil {
		caps.PackageManagerErr = errNilPackageManager
	} else if s, ok := pm.(fmt.Stringer); ok {
		caps.PackageManager = s.String()
	} else {
		caps.PackageManager = fmt.Sprintf("%T", pm)
	}

	if is, err := c.ServiceManager(); err != nil {
		caps.InitSystemErr = err
	} else if is == nil {
		caps.InitSystemErr = errNilInitSystem
	} else if s, ok := is.(fmt.Stringer); ok {
		caps.InitSystem = s.String()
	} else {
		caps.InitSystem = fmt.Sprintf("%T", is)
	}

	caps.Sudo, caps.SudoErr = sudoAvailable(c.SudoRunner())

	if c.connection != nil {
		_, caps.InteractiveExec = c.connection.(protocol.InteractiveExecer)
	}

	return caps
}

// CheckSudo eagerly validates that privileged command execution is available
// on the remote host. It returns nil if [Client.Sudo] would succeed, or a
// wrapped error describing why privilege escalation is unavailable.
//
// A context is accepted for API consistency and to allow future
// implementations to perform an active liveness check, but is not used today.
func (c *Client) CheckSudo(_ context.Context) error {
	_, err := sudoAvailable(c.SudoRunner())
	if err != nil {
		return fmt.Errorf("privilege check failed: %w", err)
	}
	return nil
}
