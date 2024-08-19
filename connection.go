// Package rig provides an easy way to add multi-protocol connectivity and
// multi-os operation support to your application's Host objects
package rig

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/alessio/shellescape"
	"github.com/creasty/defaults"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/log"
	rigos "github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/pkg/rigfs"
	"github.com/mattn/go-shellwords"
)

var _ rigos.Host = (*Connection)(nil)

type client interface {
	Connect() error
	Disconnect()
	IsWindows() bool
	Exec(cmd string, opts ...exec.Option) error
	ExecStreams(cmd string, stdin io.ReadCloser, stdout io.Writer, stderr io.Writer, opts ...exec.Option) (exec.Waiter, error)
	ExecInteractive(cmd string) error
	String() string
	Protocol() string
	IPAddress() string
	IsConnected() bool
}

type sudofn func(string) string

// Connection is a Struct you can embed into your application's "Host" types
// to give them multi-protocol connectivity.
//
// All of the important fields have YAML tags.
//
// If you have a host like this:
//
//	type Host struct {
//	  rig.Connection `yaml:"connection"`
//	}
//
// and a YAML like this:
//
//	hosts:
//	  - connection:
//	      ssh:
//	        address: 10.0.0.1
//	        port: 8022
//
// you can then simply do this:
//
//	var hosts []*Host
//	if err := yaml.Unmarshal(data, &hosts); err != nil {
//	  panic(err)
//	}
//	for _, h := range hosts {
//	  err := h.Connect()
//	  if err != nil {
//	    panic(err)
//	  }
//	  output, err := h.ExecOutput("echo hello")
//	}
type Connection struct {
	WinRM     *WinRM     `yaml:"winRM,omitempty"`
	SSH       *SSH       `yaml:"ssh,omitempty"`
	Localhost *Localhost `yaml:"localhost,omitempty"`
	OpenSSH   *OpenSSH   `yaml:"openSSH,omitempty"`

	OSVersion *OSVersion `yaml:"-"`

	client   client `yaml:"-"`
	sudofunc sudofn
	fsys     rigfs.Fsys
	sudofsys rigfs.Fsys
}

// SetDefaults sets a connection
func (c *Connection) SetDefaults() {
	if c.client == nil {
		c.client = c.configuredClient()
		if c.client == nil {
			c.client = defaultClient()
		}
		_ = defaults.Set(c.client)
	}
}

// Protocol returns the connection protocol name
func (c *Connection) Protocol() string {
	if c.client != nil {
		return c.client.Protocol()
	}

	if client := c.configuredClient(); client != nil {
		return client.Protocol()
	}

	return ""
}

// Address returns the connection address
func (c *Connection) Address() string {
	if c.client != nil {
		return c.client.IPAddress()
	}

	if client := c.configuredClient(); client != nil {
		return client.IPAddress()
	}

	return ""
}

// IsConnected returns true if the client is assumed to be connected.
// "Assumed" - as in `Connect()` has been called and no error was returned.
// The underlying client may actually have disconnected and has become
// inoperable, but rig won't know that until you try to execute commands on
// the connection.
func (c *Connection) IsConnected() bool {
	if c.client == nil {
		return false
	}

	return c.client.IsConnected()
}

func (c *Connection) checkConnected() error {
	if !c.IsConnected() {
		return ErrNotConnected
	}

	return nil
}

// String returns a printable representation of the connection, which will look
// like: `[ssh] address:port`
func (c Connection) String() string {
	if c.client == nil {
		return fmt.Sprintf("[%s] %s", c.Protocol(), c.Address())
	}

	return c.client.String()
}

// Fsys returns a fs.FS compatible filesystem interface for accessing files on remote hosts
func (c *Connection) Fsys() rigfs.Fsys {
	if c.fsys == nil {
		c.fsys = rigfs.NewFsys(c)
	}

	return c.fsys
}

// SudoFsys returns a fs.FS compatible filesystem interface for accessing files on remote hosts with sudo permissions
func (c *Connection) SudoFsys() rigfs.Fsys {
	if c.sudofsys == nil {
		c.sudofsys = rigfs.NewFsys(c, exec.Sudo(c))
	}

	return c.sudofsys
}

// IsWindows returns true on windows hosts
func (c *Connection) IsWindows() bool {
	if c.OSVersion != nil {
		return c.OSVersion.ID == "windows"
	}
	if !c.IsConnected() {
		if client := c.configuredClient(); client != nil {
			return client.IsWindows()
		}
	}
	return c.client.IsWindows()
}

// ExecStreams executes a command on the remote host and uses the passed in streams for stdin, stdout and stderr. It returns a Waiter with a .Wait() function that
// blocks until the command finishes and returns an error if the exit code is not zero.
func (c Connection) ExecStreams(cmd string, stdin io.ReadCloser, stdout, stderr io.Writer, opts ...exec.Option) (exec.Waiter, error) {
	if err := c.checkConnected(); err != nil {
		return nil, fmt.Errorf("%w: exec with streams: %w", ErrCommandFailed, err)
	}
	waiter, err := c.client.ExecStreams(cmd, stdin, stdout, stderr, opts...)
	if err != nil {
		return nil, fmt.Errorf("%w: exec with streams: %w", ErrCommandFailed, err)
	}
	return waiter, nil
}

// Exec runs a command on the host
func (c Connection) Exec(cmd string, opts ...exec.Option) error {
	if err := c.checkConnected(); err != nil {
		return err
	}

	if err := c.client.Exec(cmd, opts...); err != nil {
		return fmt.Errorf("%w: client exec: %w", ErrCommandFailed, err)
	}

	return nil
}

// ExecOutput runs a command on the host and returns the output as a String
func (c Connection) ExecOutput(cmd string, opts ...exec.Option) (string, error) {
	if err := c.checkConnected(); err != nil {
		return "", err
	}

	var output string
	opts = append(opts, exec.Output(&output))
	err := c.Exec(cmd, opts...)
	return strings.TrimSpace(output), err
}

// Connect to the host and identify the operating system and sudo capability
func (c *Connection) Connect() error {
	if c.client == nil {
		if err := defaults.Set(c); err != nil {
			return fmt.Errorf("%w: set defaults: %w", ErrValidationFailed, err)
		}
	}

	if err := c.client.Connect(); err != nil {
		c.client = nil
		log.Debugf("%s: failed to connect: %v", c, err)
		return fmt.Errorf("%w: client connect: %w", ErrNotConnected, err)
	}

	if c.OSVersion == nil {
		o, err := GetOSVersion(c)
		if err != nil {
			return err
		}
		c.OSVersion = &o
	}

	c.configureSudo()

	return nil
}

func sudoNoop(cmd string) string {
	return cmd
}

func sudoSudo(cmd string) string {
	parts, err := shellwords.Parse(cmd)
	if err != nil {
		return "sudo -s -- " + cmd
	}

	var idx int
	for i, p := range parts {
		if strings.Contains(p, "=") {
			idx = i + 1
			continue
		}
		break
	}

	if idx == 0 {
		return "sudo -s -- " + cmd
	}

	for i, p := range parts {
		parts[i] = shellescape.Quote(p)
	}

	return fmt.Sprintf("sudo -s %s -- %s", strings.Join(parts[0:idx], " "), strings.Join(parts[idx:], " "))
}

func sudoDoas(cmd string) string {
	return `doas -n -- "${SHELL-sh}" -c ` + shellescape.Quote(cmd)
}

// sudoWindows is a no-op on windows - the user must already be an admin or UAC must be disabled
// and the user must belong to Administrators. if that is the case, the user should be able to
// do anything.
func sudoWindows(cmd string) string {
	return cmd
}

func (c *Connection) configureSudo() {
	if !c.IsWindows() {
		if c.Exec(`[ "$(id -u)" = 0 ]`) == nil {
			// user is already root
			c.sudofunc = sudoNoop
			return
		}
		if c.Exec(`sudo -n -l`) == nil {
			// user has passwordless sudo
			c.sudofunc = sudoSudo
			return
		}
		if c.Exec(`doas -n -- "${SHELL-sh}" -c true`) == nil {
			// user has passwordless doas
			c.sudofunc = sudoDoas
		}
		return
	}

	out, err := c.ExecOutput(`whoami`)
	if err != nil {
		return
	}
	parts := strings.Split(out, `\`)
	if strings.ToLower(parts[len(parts)-1]) == "administrator" {
		// user is already the administrator
		c.sudofunc = sudoWindows
		return
	}

	if c.Exec(`net user "%USERNAME%" | findstr /B /C:"Local Group Memberships" | findstr /C:"*Administrators"`) != nil {
		// user is not in the Administrators group
		return
	}

	out, err = c.ExecOutput(`reg query "HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\System" /v "EnableLUA"`)
	if err != nil {
		return
	}
	if strings.Contains(out, "0x0") {
		// UAC is disabled and the user is in the Administrators group - expect sudo to work
		c.sudofunc = sudoWindows
		return
	}
}

// Sudo formats a command string to be run with elevated privileges
func (c Connection) Sudo(cmd string) (string, error) {
	if c.sudofunc == nil {
		if c.IsWindows() {
			return "", fmt.Errorf("%w: UAC is enabled and user is not 'Administrator'", ErrSudoRequired)
		}
		return "", fmt.Errorf("%w: user is not root and passwordless access elevation (sudo, doas) has not been configured", ErrSudoRequired)
	}

	return c.sudofunc(cmd), nil
}

// Execf is just like `Exec` but you can use Sprintf templating for the command
func (c Connection) Execf(s string, params ...any) error {
	opts, args := GroupParams(params...)
	return c.Exec(fmt.Sprintf(s, args...), opts...)
}

// ExecOutputf is like ExecOutput but you can use Sprintf
// templating for the command
func (c Connection) ExecOutputf(s string, params ...any) (string, error) {
	opts, args := GroupParams(params...)
	return c.ExecOutput(fmt.Sprintf(s, args...), opts...)
}

// ExecInteractive executes a command on the host and passes control of
// local input to the remote command
func (c Connection) ExecInteractive(cmd string) error {
	if err := c.checkConnected(); err != nil {
		return err
	}

	if err := c.client.ExecInteractive(cmd); err != nil {
		return fmt.Errorf("%w: client exec interactive: %w", ErrCommandFailed, err)
	}

	return nil
}

// Disconnect from the host
func (c *Connection) Disconnect() {
	if c.client != nil {
		c.client.Disconnect()
	}
	c.client = nil
}

// Upload copies a file from a local path src to the remote host path dst. For
// smaller files you should probably use os.WriteFile
func (c *Connection) Upload(src, dst string, opts ...exec.Option) error {

	if err := c.checkConnected(); err != nil {
		return err
	}
	local, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidPath, err)
	}
	defer local.Close()

	stat, err := local.Stat()
	if err != nil {
		return fmt.Errorf("%w: stat local file %s: %w", ErrInvalidPath, src, err)
	}

	shasum := sha256.New()

	fsys := rigfs.NewFsys(c, opts...)
	remote, err := fsys.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, stat.Mode())
	if err != nil {
		return fmt.Errorf("%w: open remote file %s for writing: %w", ErrInvalidPath, dst, err)
	}
	defer remote.Close()

	localReader := io.TeeReader(local, shasum)
	if _, err := remote.CopyFrom(localReader); err != nil {
		_ = remote.Close()
		return fmt.Errorf("%w: copy file %s to remote host: %w", ErrUploadFailed, dst, err)
	}
	if err := remote.Close(); err != nil {
		return fmt.Errorf("%w: close remote file %s: %w", ErrUploadFailed, dst, err)
	}

	log.Debugf("%s: post-upload validate checksum of %s", c, dst)
	remoteSum, err := fsys.Sha256(dst)
	if err != nil {
		return fmt.Errorf("%w: validate %s checksum: %w", ErrUploadFailed, dst, err)
	}

	if remoteSum != hex.EncodeToString(shasum.Sum(nil)) {
		return fmt.Errorf("%w: checksum mismatch", ErrUploadFailed)
	}

	return nil
}

func (c *Connection) configuredClient() client {
	if c.WinRM != nil {
		return c.WinRM
	}

	if c.Localhost != nil {
		return c.Localhost
	}

	if c.SSH != nil {
		return c.SSH
	}

	if c.OpenSSH != nil {
		return c.OpenSSH
	}

	return nil
}

func defaultClient() client {
	return &Localhost{Enabled: true}
}

// GroupParams separates exec.Options from other sprintf templating args
func GroupParams(params ...any) ([]exec.Option, []any) {
	var opts []exec.Option
	var args []any
	for _, v := range params {
		switch vv := v.(type) {
		case []any:
			o, a := GroupParams(vv...)
			opts = append(opts, o...)
			args = append(args, a...)
		case exec.Option:
			opts = append(opts, vv)
		default:
			args = append(args, vv)
		}
	}
	return opts, args
}
