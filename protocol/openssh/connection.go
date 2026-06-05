// Package openssh provides a rig protocol implementation that uses the system's openssh client "ssh" to connect to remote hosts.
package openssh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/k0sproject/rig/v2/log"
	"github.com/k0sproject/rig/v2/protocol"
	"github.com/k0sproject/rig/v2/sshconfig"
)

var (
	// ErrControlPathNotSet is returned when the controlpath is not set when disconnecting from a multiplexed connection.
	ErrControlPathNotSet = errors.New("controlpath not set")

	errNotConnected = errors.New("not connected")
)

// isHostKeyError reports whether ssh stderr output indicates a host key
// verification failure. These are fatal and should not be retried.
func isHostKeyError(stderr string) bool {
	return strings.Contains(stderr, "Host key verification failed") ||
		strings.Contains(stderr, "REMOTE HOST IDENTIFICATION HAS CHANGED")
}

// Connection is a rig.Connection implementation that uses the system openssh client "ssh" to connect to remote hosts.
// The connection is by default multiplexec over a control master, so that subsequent connections don't need to re-authenticate.
type Connection struct {
	log.LoggerInjectable `yaml:"-"`
	Config               `yaml:",inline"`

	isConnected  bool
	controlPath  string
	controlMutex sync.Mutex

	isWindows *bool

	name string
}

// NewConnection creates a new OpenSSH connection. Error is currently always nil.
func NewConnection(cfg Config) (*Connection, error) {
	cfg.SetDefaults()
	return &Connection{Config: cfg}, nil
}

// Protocol returns the protocol family, "SSH".
func (c *Connection) Protocol() string {
	return "SSH"
}

// ProtocolName returns the implementation name, "OpenSSH".
func (c *Connection) ProtocolName() string {
	return "OpenSSH"
}

// IPAddress returns the IP address of the remote host.
func (c *Connection) IPAddress() string {
	return c.Address
}

// detectWindows probes the remote host to determine whether it is running
// Windows and stores the result in the cache. The caller is responsible for
// ensuring the connection is established before calling this method.
func (c *Connection) detectWindows(ctx context.Context) bool {
	isWinProc, err := c.StartProcess(ctx, "cmd.exe /c exit 0", nil, nil, nil)
	if err != nil {
		// Probe couldn't start (e.g. not yet connected); don't cache so a
		// subsequent call after Connect can succeed.
		return false
	}
	isWin := isWinProc.Wait() == nil
	// Don't cache when the context was cancelled — the probe result may
	// reflect cancellation rather than the actual remote OS.
	if ctx.Err() != nil {
		return false
	}
	c.controlMutex.Lock()
	c.isWindows = &isWin
	c.controlMutex.Unlock()
	log.Trace(ctx, fmt.Sprintf("host is windows: %t", isWin), log.KeyHost, c)
	return isWin
}

// IsWindows returns true if the remote host is windows.
// The result is cached after the first probe; subsequent calls are O(1).
// For reliable context propagation, Connect should be called first —
// detection is also triggered during Connect using the connect context.
func (c *Connection) IsWindows() bool {
	c.controlMutex.Lock()
	if c.isWindows != nil {
		result := *c.isWindows
		c.controlMutex.Unlock()
		return result
	}
	c.controlMutex.Unlock()

	// Fallback: probe with a bounded background context.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return c.detectWindows(ctx)
}

// DefaultOptionArguments are the default options for the OpenSSH client.
var DefaultOptionArguments = sshconfig.OptionArguments{
	// It's easy to end up with control paths that are too long for unix sockets (104 chars?)
	// with the default ~/.ssh/master-%r@%h:%p, for example something like:
	// /Users/user/.ssh/master-ec2-xx-xx-xx-xx.eu-central-1.compute.amazonaws.com-centos.AAZFTHkT5....
	// so, using %C here for hash instead.
	//
	// Note that openssh client does not respect $HOME so this will always be in the actual home dir
	// that the ssh client digs from /etc/passwd.
	"ControlPath":           "~/.ssh/ctrl-%C",
	"ControlMaster":         false,
	"ServerAliveInterval":   "60",
	"ServerAliveCountMax":   "3",
	"StrictHostKeyChecking": false,
	"Compression":           false,
	"ConnectTimeout":        "10",
}

// SetDefaults sets default values.
func (c *Connection) SetDefaults() {
	if c.Options == nil {
		c.Options = make(sshconfig.OptionArguments)
	}
	for key, val := range DefaultOptionArguments {
		if val == nil {
			delete(c.Options, key)
			c.Log().Debug("removing option (default is nil/delete)", "key", key)
			continue
		}
		if c.Options.IsSet(key) {
			c.Log().Debug("keeping user-supplied option (skipping default)", "key", key)
			continue
		}
		c.Options.SetIfUnset(key, val)
		c.Log().Debug("applied default option", "key", key)
	}
	if c.DisableMultiplexing {
		delete(c.Options, "ControlMaster")
		delete(c.Options, "ControlPath")
		c.Log().Debug("multiplexing disabled — removed ControlMaster and ControlPath")
	}
}

func (c *Connection) userhost() string {
	if c.User != nil {
		return fmt.Sprintf("%s@%s", *c.User, c.Address)
	}
	return c.Address
}

func (c *Connection) args() []string {
	args := []string{}
	if c.KeyPath != nil && *c.KeyPath != "" {
		args = append(args, "-i", *c.KeyPath)
	}
	if c.Port != nil {
		args = append(args, "-p", strconv.Itoa(*c.Port))
	}
	if c.ConfigPath != nil && *c.ConfigPath != "" {
		args = append(args, "-F", *c.ConfigPath)
	}
	args = append(args, c.userhost())
	return args
}

// Connect connects to the remote host. If multiplexing is enabled, this will start a control master. If multiplexing is disabled, this will just run a noop command to check connectivity.
func (c *Connection) Connect(ctx context.Context) error {
	c.controlMutex.Lock()
	if c.isConnected {
		c.controlMutex.Unlock()
		return nil
	}

	if c.DisableMultiplexing {
		c.controlMutex.Unlock()
		// Run a noop to check connectivity. Capture stderr to detect host key failures.
		errBuf := bytes.NewBuffer(nil)
		proc, err := c.StartProcess(ctx, "exit 0", nil, nil, errBuf)
		if err == nil {
			err = proc.Wait()
		}
		if err != nil {
			errOut := errBuf.String()
			if isHostKeyError(errOut) {
				return fmt.Errorf("%w: host key verification failed: %w (%s)", protocol.ErrNonRetryable, err, errOut)
			}
			return fmt.Errorf("failed to connect: %w", err)
		}
		c.controlMutex.Lock()
		c.isConnected = true
		c.controlMutex.Unlock()
		c.prewarmWindows(ctx)
		return nil
	}

	// Multiplexing path: manage the lock explicitly so we can release it
	// before calling detectWindows (StartProcess acquires the same lock).
	opts := c.Options.Copy()
	opts.Set("ControlMaster", true)
	opts.Set("ControlPersist", 600)
	opts.Set("TCPKeepalive", true)

	args := make([]string, 0, 2+len(opts.ToArgs())+len(c.args()))
	args = append(args, "-N", "-f")
	args = append(args, opts.ToArgs()...)
	args = append(args, c.args()...)

	cmd := exec.CommandContext(ctx, "ssh", args...)
	errBuf := bytes.NewBuffer(nil)
	cmd.Stderr = errBuf

	log.Trace(ctx, "starting ssh control master", log.KeyHost, c, log.KeyCommand, strings.Join(args, " "))
	if err := cmd.Run(); err != nil {
		c.isConnected = false
		c.controlMutex.Unlock()
		errOut := errBuf.String()
		if isHostKeyError(errOut) {
			return fmt.Errorf("%w: host key verification failed: %w (%s)", protocol.ErrNonRetryable, err, errOut)
		}
		return fmt.Errorf("failed to start ssh multiplexing control master: %w (%s)", err, errOut)
	}

	c.isConnected = true
	if cp, ok := c.Options["ControlPath"].(string); ok {
		c.controlPath = cp
	}
	log.Trace(ctx, "started ssh multiplexing control master", log.KeyHost, c)
	c.controlMutex.Unlock()

	c.prewarmWindows(ctx)

	return nil
}

// prewarmWindows calls detectWindows with a short bounded context derived from
// ctx so that Connect does not block indefinitely on the OS probe. A cancelled
// or expired ctx causes the probe to be skipped entirely, leaving the cache
// empty (IsWindows will probe on first call instead).
func (c *Connection) prewarmWindows(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	c.detectWindows(probeCtx)
}

func (c *Connection) closeControl() error {
	c.controlMutex.Lock()
	defer c.controlMutex.Unlock()

	if !c.isConnected {
		return nil
	}

	if c.controlPath == "" {
		return ErrControlPathNotSet
	}

	args := make([]string, 0, 4+len(c.args()))
	args = append(args, "-O", "exit", "-S", c.controlPath)
	args = append(args, c.args()...)

	log.Trace(context.Background(), "closing ssh multiplexing control master", log.KeyHost, c)
	cmd := exec.Command("ssh", args...) //nolint:noctx // cleanup code path, no context available
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to close ssh control master: %w", err)
	}
	c.isConnected = false
	return nil
}

// StartProcess executes a command on the remote host, streaming stdin, stdout and stderr.
func (c *Connection) StartProcess(ctx context.Context, cmdStr string, stdin io.Reader, stdout, stderr io.Writer) (protocol.Waiter, error) {
	c.controlMutex.Lock()
	connected := c.isConnected
	c.controlMutex.Unlock()
	if !c.DisableMultiplexing && !connected {
		return nil, errNotConnected
	}

	args := c.Options.ToArgs()
	args = append(args, "-o", "BatchMode=yes")
	args = append(args, c.args()...)
	args = append(args, "--", cmdStr)
	cmd := exec.CommandContext(ctx, "ssh", args...)

	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start command: %w", err)
	}

	return cmd, nil
}

// ExecInteractive executes a command on the host and passes stdin/stdout/stderr as-is to the session.
// The session is terminated when ctx is cancelled.
func (c *Connection) ExecInteractive(ctx context.Context, cmdStr string, stdin io.Reader, stdout, stderr io.Writer) error {
	cmd, err := c.StartProcess(ctx, cmdStr, stdin, stdout, stderr)
	if err != nil {
		return err
	}
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err() //nolint:wrapcheck // context error is the real cause
		}
		return fmt.Errorf("command wait: %w", err)
	}
	return nil
}

func (c *Connection) String() string {
	if c.name != "" {
		return c.name
	}

	if c.Port == nil {
		c.name = c.userhost()
	} else {
		c.name = fmt.Sprintf("%s:%d", c.userhost(), *c.Port)
	}

	return c.name
}

// IsConnected returns true if the connection is alive. For multiplexed
// connections this probes the control master via ssh -O check. For
// non-multiplexed connections it runs a no-op command over a fresh session.
func (c *Connection) IsConnected() bool {
	c.controlMutex.Lock()
	connected := c.isConnected
	controlPath := c.controlPath
	c.controlMutex.Unlock()

	if !connected {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !c.DisableMultiplexing {
		var args []string
		if controlPath != "" {
			args = make([]string, 0, 4+len(c.args()))
			args = append(args, "-O", "check", "-S", controlPath)
		} else {
			// ControlPath comes from ssh_config (-F); let ssh resolve it from options.
			args = make([]string, 0, 2+len(c.Options.ToArgs())+len(c.args()))
			args = append(args, c.Options.ToArgs()...)
			args = append(args, "-O", "check")
		}
		args = append(args, c.args()...)
		if exec.CommandContext(ctx, "ssh", args...).Run() != nil {
			c.controlMutex.Lock()
			c.isConnected = false
			c.controlMutex.Unlock()
			return false
		}
		return true
	}
	proc, err := c.StartProcess(ctx, "exit 0", nil, nil, nil)
	if err != nil || proc.Wait() != nil {
		c.controlMutex.Lock()
		c.isConnected = false
		c.controlMutex.Unlock()
		return false
	}
	return true
}

// Disconnect disconnects from the remote host. If multiplexing is enabled, this will close the control master.
// If multiplexing is disabled, this marks the connection as disconnected.
func (c *Connection) Disconnect() {
	if c.DisableMultiplexing {
		c.controlMutex.Lock()
		c.isConnected = false
		c.controlMutex.Unlock()
		return
	}

	if err := c.closeControl(); err != nil {
		log.Trace(context.Background(), "failed to close control master", log.KeyHost, c, log.KeyError, err)
	}
}
