package rig

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	goexec "os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/log"
)

// ErrControlPathNotSet is returned when the controlpath is not set when disconnecting from a multiplexed connection
var ErrControlPathNotSet = errors.New("controlpath not set")

// OpenSSH is a rig.Connection implementation that uses the system openssh client "ssh" to connect to remote hosts.
// The connection is multiplexec over a control master, so that subsequent connections don't need to re-authenticate.
type OpenSSH struct {
	Address             string         `yaml:"address" validate:"required"`
	User                *string        `yaml:"user"`
	Port                *int           `yaml:"port"`
	KeyPath             *string        `yaml:"keyPath,omitempty"`
	ConfigPath          *string        `yaml:"configPath,omitempty"`
	Options             OpenSSHOptions `yaml:"options,omitempty"`
	DisableMultiplexing bool           `yaml:"disableMultiplexing,omitempty"`

	isConnected  bool
	controlMutex sync.Mutex

	isWindows *bool

	name string
}

// Client implements the ClientConfigurer interface
func (c *OpenSSH) Client() (Client, error) {
	return c, nil
}

// Protocol returns the protocol name
func (c *OpenSSH) Protocol() string {
	return "OpenSSH"
}

// IPAddress returns the IP address of the remote host
func (c *OpenSSH) IPAddress() string {
	return c.Address
}

// IsWindows returns true if the remote host is windows
func (c *OpenSSH) IsWindows() bool {
	// Implement your logic here
	if c.isWindows != nil {
		return *c.isWindows
	}

	var isWin bool

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	isWinProc, err := c.StartProcess(ctx, "cmd.exe /c exit 0", nil, nil, nil)
	isWin = err == nil && isWinProc.Wait() == nil

	c.isWindows = &isWin
	log.Debugf("%s: host is windows: %t", c, *c.isWindows)

	return *c.isWindows
}

// OpenSSHOptions are options for the OpenSSH client. For example StrictHostkeyChecking: false becomes -o StrictHostKeyChecking=no
type OpenSSHOptions map[string]any

// Copy returns a copy of the options
func (o OpenSSHOptions) Copy() OpenSSHOptions {
	dup := make(OpenSSHOptions, len(o))
	for k, v := range o {
		dup[k] = v
	}
	return dup
}

// Set sets an option key to value
func (o OpenSSHOptions) Set(key string, value any) {
	o[key] = value
}

// SetIfUnset sets the option if it's not already set
func (o OpenSSHOptions) SetIfUnset(key string, value any) {
	if o.IsSet(key) {
		return
	}
	o.Set(key, value)
}

// IsSet returns true if the option is set
func (o OpenSSHOptions) IsSet(key string) bool {
	_, ok := o[key]
	return ok
}

// ToArgs converts the options to command line arguments
func (o OpenSSHOptions) ToArgs() []string {
	args := make([]string, 0, len(o)*2)
	for k, v := range o {
		if b, ok := v.(bool); ok {
			if b {
				args = append(args, "-o", fmt.Sprintf("%s=yes", k))
			} else {
				args = append(args, "-o", fmt.Sprintf("%s=no", k))
			}
			continue
		}
		args = append(args, "-o", fmt.Sprintf("%s=%v", k, v))
	}
	return args
}

// DefaultOpenSSHOptions are the default options for the OpenSSH client
var DefaultOpenSSHOptions = OpenSSHOptions{
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

// SetDefaults sets default values
func (c *OpenSSH) SetDefaults() {
	if c.Options == nil {
		c.Options = make(OpenSSHOptions)
	}
	for k, v := range DefaultOpenSSHOptions {
		if v == nil {
			delete(c.Options, k)
			continue
		}
		c.Options.SetIfUnset(k, v)
	}
	if c.DisableMultiplexing {
		delete(c.Options, "ControlMaster")
		delete(c.Options, "ControlPath")
	}
}

func (c *OpenSSH) userhost() string {
	if c.User != nil {
		return fmt.Sprintf("%s@%s", *c.User, c.Address)
	}
	return c.Address
}

func (c *OpenSSH) args() []string {
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
func (c *OpenSSH) Connect() error {
	if c.isConnected {
		return nil
	}

	if c.DisableMultiplexing {
		// just run a noop command to check connectivity
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := c.StartProcess(ctx, "exit 0", nil, nil, nil); err != nil {
			return fmt.Errorf("failed to connect: %w", err)
		}
		c.isConnected = true
		return nil
	}

	c.controlMutex.Lock()
	defer c.controlMutex.Unlock()

	opts := c.Options.Copy()
	opts.Set("ControlMaster", true)
	opts.Set("ControlPersist", 600)
	opts.Set("TCPKeepalive", true)

	args := []string{"-N", "-f"}
	args = append(args, opts.ToArgs()...)
	args = append(args, c.args()...)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := goexec.CommandContext(ctx, "ssh", args...)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("create stderr pipe: %w", err)
	}
	defer stderr.Close()
	errBuf := bytes.NewBuffer(nil)
	go func() {
		_, _ = io.Copy(errBuf, stderr)
	}()

	log.Debugf("%s: starting ssh control master using 'ssh %s'", c, strings.Join(args, " "))
	if err := cmd.Run(); err != nil {
		c.isConnected = false
		return fmt.Errorf("failed to start ssh multiplexing control master: %w (%s)", err, errBuf.String())
	}

	c.isConnected = true
	log.Debugf("%s: started ssh multipliexing control master", c)

	return nil
}

func (c *OpenSSH) closeControl() error {
	c.controlMutex.Lock()
	defer c.controlMutex.Unlock()

	if !c.isConnected {
		return nil
	}

	controlPath, ok := c.Options["ControlPath"].(string)
	if !ok {
		return ErrControlPathNotSet
	}

	args := []string{"-O", "exit", "-S", controlPath}
	args = append(args, c.args()...)
	args = append(args, c.userhost())

	log.Debugf("%s: closing ssh multiplexing control master", c)
	cmd := goexec.Command("ssh", args...)
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to close control master: %w", err)
	}
	c.isConnected = false
	return nil
}

// StartProcess executes a command on the remote host, streaming stdin, stdout and stderr
func (c *OpenSSH) StartProcess(ctx context.Context, cmdStr string, stdin io.Reader, stdout, stderr io.Writer) (exec.Waiter, error) {
	if !c.DisableMultiplexing && !c.isConnected {
		return nil, ErrNotConnected
	}

	args := c.Options.ToArgs()
	args = append(args, "-o", "BatchMode=yes")
	args = append(args, c.args()...)
	args = append(args, "--", cmdStr)
	cmd := goexec.CommandContext(ctx, "ssh", args...)

	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%w: failed to start: %w", ErrCommandFailed, err)
	}

	return cmd, nil
}

// ExecInteractive executes an interactive command on the remote host, streaming stdin, stdout and stderr
func (c *OpenSSH) ExecInteractive(cmdStr string, stdin io.Reader, stdout, stderr io.Writer) error {
	cmd, err := c.StartProcess(context.Background(), cmdStr, stdin, stdout, stderr)
	if err != nil {
		return err
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("command wait: %w", err)
	}
	return nil
}

func (c *OpenSSH) String() string {
	if c.name != "" {
		return c.name
	}

	c.name = fmt.Sprintf("[OpenSSH] %s", c.userhost())
	if c.Port != nil {
		c.name = fmt.Sprintf("%s:%d", c.name, *c.Port)
	}

	return c.name
}

// IsConnected returns true if the connection is connected
func (c *OpenSSH) IsConnected() bool {
	return c.isConnected
}

// Disconnect disconnects from the remote host. If multiplexing is enabled, this will close the control master.
// If multiplexing is disabled, this will do nothing.
func (c *OpenSSH) Disconnect() {
	if c.DisableMultiplexing {
		// nothing to do
		return
	}

	if err := c.closeControl(); err != nil {
		log.Warnf("%s: failed to close control master: %v", c, err)
	}
}
