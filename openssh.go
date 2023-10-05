package rig

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	goexec "os/exec"
	"strconv"
	"strings"
	"sync"

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

func boolPtr(b bool) *bool {
	return &b
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

	c.isWindows = boolPtr(c.Exec("cmd.exe /c exit 0") == nil)
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
		if err := c.Exec("exit 0", exec.StreamOutput()); err != nil {
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

	cmd := goexec.Command("ssh", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	log.Debugf("%s: starting ssh control master", c)
	err := cmd.Run()
	if err != nil {
		c.isConnected = false
		return fmt.Errorf("failed to start ssh multiplexing control master: %w", err)
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

// Exec executes a command on the remote host
func (c *OpenSSH) Exec(cmdStr string, opts ...exec.Option) error { //nolint:cyclop
	if !c.DisableMultiplexing && !c.isConnected {
		return ErrNotConnected
	}

	execOpts := exec.Build(opts...)
	command, err := execOpts.Command(cmdStr)
	if err != nil {
		return fmt.Errorf("failed to build command: %w", err)
	}

	args := c.Options.ToArgs()
	// use BatchMode (no password prompts) for non-interactive commands
	args = append(args, "-o", "BatchMode=yes")
	args = append(args, c.args()...)
	args = append(args, "--", command)
	cmd := goexec.Command("ssh", args...)

	if execOpts.Stdin != "" {
		execOpts.LogStdin(c.String())
		cmd.Stdin = strings.NewReader(execOpts.Stdin)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	execOpts.LogCmd(c.String(), cmd.String())

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start command: %w", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()

		if execOpts.Writer == nil {
			outputScanner := bufio.NewScanner(stdout)

			for outputScanner.Scan() {
				execOpts.AddOutput(c.String(), outputScanner.Text()+"\n", "")
			}
			if err := outputScanner.Err(); err != nil {
				execOpts.LogErrorf("%s: failed to scan stdout: %v", c, err)
			}
		} else {
			if _, err := io.Copy(execOpts.Writer, stdout); err != nil {
				execOpts.LogErrorf("%s: failed to stream stdout: %v", c, err)
			}
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()

		outputScanner := bufio.NewScanner(stderr)

		for outputScanner.Scan() {
			execOpts.AddOutput(c.String(), "", outputScanner.Text()+"\n")
		}
		if err := outputScanner.Err(); err != nil {
			execOpts.LogErrorf("%s: failed to scan stderr: %v", c, err)
		}
	}()

	wg.Wait()
	err = cmd.Wait()
	if err != nil {
		return fmt.Errorf("%w: command wait: %w", ErrCommandFailed, err)
	}
	return nil
}

// ExecStreams executes a command on the remote host, streaming stdin, stdout and stderr
func (c *OpenSSH) ExecStreams(cmdStr string, stdin io.ReadCloser, stdout, stderr io.Writer, opts ...exec.Option) (waiter, error) {
	if !c.DisableMultiplexing && !c.isConnected {
		return nil, ErrNotConnected
	}
	execOpts := exec.Build(opts...)
	command, err := execOpts.Command(cmdStr)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to build command: %w", ErrCommandFailed, err)
	}

	args := c.Options.ToArgs()
	args = append(args, "-o", "BatchMode=yes")
	args = append(args, c.args()...)
	args = append(args, "--", command)
	cmd := goexec.Command("ssh", args...)

	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	execOpts.LogCmd(c.String(), cmd.String())

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%w: failed to start: %w", ErrCommandFailed, err)
	}

	return cmd, nil
}

// ExecInteractive executes an interactive command on the remote host, streaming stdin, stdout and stderr
func (c *OpenSSH) ExecInteractive(cmdStr string) error {
	cmd, err := c.ExecStreams(cmdStr, os.Stdin, os.Stdout, os.Stderr)
	if err != nil {
		return err
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("%w: command wait: %w", ErrCommandFailed, err)
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

	c.controlMutex.Lock()
	defer c.controlMutex.Unlock()

	if !c.isConnected {
		return
	}

	if err := c.closeControl(); err != nil {
		log.Warnf("%s: failed to close control master: %v", c, err)
	}
}
