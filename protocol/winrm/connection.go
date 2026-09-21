// Package winrm provides a rig protocol implementation for WinRM connections
package winrm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/k0sproject/rig/v2/log"
	"github.com/k0sproject/rig/v2/protocol"
	"github.com/k0sproject/rig/v2/protocol/ssh"
	"github.com/k0sproject/rig/v2/protocol/ssh/hostkey"
	"github.com/masterzen/winrm"
)

var (
	errExitCode       = errors.New("command exited with a non-zero exit code")
	errNotConnected   = errors.New("not connected")
	errInvalidCommand = errors.New("invalid command")
)

// Connection describes a Connection connection with its configuration options.
type Connection struct {
	log.LoggerInjectable `yaml:"-"`
	Config               `yaml:",inline"`

	name string

	caCert []byte
	key    []byte
	cert   []byte

	mu          sync.Mutex
	client      *winrm.Client
	bastionConn *ssh.Connection
}

type dialFunc func(network, addr string) (net.Conn, error)

// NewConnection creates a new WinRM connection. Error is currently always nil.
func NewConnection(cfg Config, opts ...Option) (*Connection, error) {
	options := NewOptions(opts...)
	options.InjectLoggerTo(cfg, log.KeyProtocol, "winrm-config")
	cfg.SetDefaults()

	c := &Connection{Config: cfg}
	options.InjectLoggerTo(c, log.KeyProtocol, "winrm")

	return c, nil
}

// Protocol returns the protocol family, "WinRM".
func (c *Connection) Protocol() string {
	return "WinRM"
}

// ProtocolName returns the implementation name, "WinRM".
func (c *Connection) ProtocolName() string {
	return "WinRM"
}

// IPAddress returns the connection address.
func (c *Connection) IPAddress() string {
	return c.Address
}

// String returns the connection's printable name.
func (c *Connection) String() string {
	if c.name == "" {
		c.name = net.JoinHostPort(c.Address, strconv.Itoa(c.Port))
	}

	return c.name
}

// IsWindows always returns true on winrm.
func (c *Connection) IsWindows() bool {
	return true
}

// probe runs a no-op command to verify the connection is alive and authenticated.
// HTTP 401/403 responses are tagged with protocol.ErrAuthFailed.
func (c *Connection) probe(ctx context.Context) error {
	proc, err := c.StartProcess(ctx, "cmd.exe /c exit 0", nil, nil, nil)
	if err != nil {
		if isAuthError(err) {
			return fmt.Errorf("%w: %w", protocol.ErrAuthFailed, err)
		}
		return err
	}
	if err := proc.Wait(); err != nil {
		return fmt.Errorf("probe: %w", err)
	}
	return nil
}

// isAuthError reports whether err looks like an HTTP 401/403 response from the
// WinRM library. The library returns untyped formatted errors so we match by
// substring. Both formats seen in the library are covered:
//   - "http error 401: ..."       (http.go / auth.go)
//   - "http response error: 401 - ..." (auth.go alternate path)
func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "http error 401") ||
		strings.Contains(msg, "http error 403") ||
		strings.Contains(msg, "http response error: 401") ||
		strings.Contains(msg, "http response error: 403")
}

// endpointTimeout returns the WinRM HTTP transport timeout to use. It returns
// time.Minute, the remaining context deadline if shorter, or 1ms if the context
// is already canceled or expired (so the HTTP call fails quickly).
func endpointTimeout(ctx context.Context) time.Duration {
	if ctx.Err() != nil {
		return time.Millisecond
	}
	timeout := time.Minute
	if deadline, ok := ctx.Deadline(); ok {
		if d := time.Until(deadline); d > 0 && d < timeout {
			return d
		}
	}
	return timeout
}

// IsConnected returns true if the WinRM connection is alive by running a no-op
// command. WinRM is stateless HTTP so the only real liveness test is a probe.
func (c *Connection) IsConnected() bool {
	c.mu.Lock()
	connected := c.client != nil
	c.mu.Unlock()
	if !connected {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return c.probe(ctx) == nil
}

func (c *Connection) loadCertificates() error {
	c.caCert = nil
	if c.CACertPath != "" {
		ca, err := os.ReadFile(c.CACertPath)
		if err != nil {
			return fmt.Errorf("load ca-cerrt %s: %w", c.CACertPath, err)
		}
		c.caCert = ca
	}

	c.cert = nil
	if c.CertPath != "" {
		cert, err := os.ReadFile(c.CertPath)
		if err != nil {
			return fmt.Errorf("load cert %s: %w", c.CertPath, err)
		}
		c.cert = cert
	}

	c.key = nil
	if c.KeyPath != "" {
		key, err := os.ReadFile(c.KeyPath)
		if err != nil {
			return fmt.Errorf("load key %s: %w", key, err)
		}
		c.key = key
	}

	return nil
}

func (c *Connection) bastionDialer(ctx context.Context) (*ssh.Connection, dialFunc, error) {
	bastion, err := c.Bastion.Connection() //nolint:contextcheck
	if err != nil {
		return nil, nil, fmt.Errorf("create bastion connection: %w", err)
	}
	bastionSSH, ok := bastion.(*ssh.Connection)
	if !ok {
		return nil, nil, fmt.Errorf("%w: bastion connection is not an SSH connection", protocol.ErrNonRetryable)
	}
	log.Trace(ctx, "connecting to bastion", log.KeyHost, c)
	if err := bastionSSH.Connect(ctx); err != nil {
		if errors.Is(err, hostkey.ErrHostKeyMismatch) {
			return nil, nil, fmt.Errorf("%w: bastion connect: %w", protocol.ErrNonRetryable, err)
		}
		return nil, nil, fmt.Errorf("bastion connect: %w", err)
	}
	return bastionSSH, bastionSSH.Dial, nil
}

// Connect opens the WinRM connection. Any existing connection is torn down first.
func (c *Connection) Connect(ctx context.Context) error {
	c.Disconnect()

	if err := c.loadCertificates(); err != nil {
		return fmt.Errorf("%w: failed to load certificates: %w", protocol.ErrNonRetryable, err)
	}

	endpoint := &winrm.Endpoint{
		Host:          c.Address,
		Port:          c.Port,
		HTTPS:         c.UseHTTPS,
		Insecure:      c.Insecure,
		TLSServerName: c.TLSServerName,
		Timeout:       endpointTimeout(ctx),
	}

	if len(c.caCert) > 0 {
		endpoint.CACert = c.caCert
	}

	if len(c.cert) > 0 {
		endpoint.Cert = c.cert
	}

	if len(c.key) > 0 {
		endpoint.Key = c.key
	}

	params := winrm.DefaultParameters

	if c.Bastion != nil {
		bastionSSH, dialer, err := c.bastionDialer(ctx)
		if err != nil {
			return err
		}
		params.Dial = dialer
		c.mu.Lock()
		c.bastionConn = bastionSSH
		c.mu.Unlock()
	}

	if c.UseNTLM {
		params.TransportDecorator = func() winrm.Transporter { return &winrm.ClientNTLM{} }
	}

	if c.UseHTTPS && len(c.cert) > 0 {
		params.TransportDecorator = func() winrm.Transporter { return &winrm.ClientAuthRequest{} }
	}

	client, err := winrm.NewClientWithParameters(endpoint, c.User, c.Password, params)
	if err != nil {
		c.Disconnect()
		return fmt.Errorf("create winrm client: %w", err)
	}

	c.mu.Lock()
	c.client = client
	c.mu.Unlock()

	if err := c.probe(ctx); err != nil {
		c.Disconnect()
		return err
	}

	return nil
}

// Disconnect closes the WinRM connection.
func (c *Connection) Disconnect() {
	c.mu.Lock()
	c.client = nil
	bastion := c.bastionConn
	c.bastionConn = nil
	c.mu.Unlock()
	if bastion != nil {
		bastion.Disconnect()
	}
}

// detachableWriter forwards writes to w until it is detached, after which it
// accepts and drops them. StartProcess gives one to each of the goroutines
// copying a command's output, so that a Wait which gives up on a command the
// host is no longer answering can cut them loose from the caller's writers:
// those goroutines outlive Wait, and detaching is what keeps them off the
// caller's writers afterwards. It is not a full handover: see StartProcess
// for what a caller may and may not do with those writers once Wait has
// returned a context error.
//
// detach never blocks, and so cannot stop a write that is already inside the
// caller's writer -- an io.Writer offers no way to interrupt one. That is
// deliberate: a writer whose Write can block is one the caller is still
// reading from at the other end (ExecReaderContext's pipe), not one it is
// about to recycle, and waiting for it here would leave Wait unbounded again,
// which is the whole thing this is meant to fix.
type detachableWriter struct {
	detached atomic.Bool
	// w is set once, at construction, and only read afterwards.
	w io.Writer
}

func (d *detachableWriter) Write(p []byte) (int, error) {
	if d.detached.Load() {
		return len(p), nil
	}
	return d.w.Write(p) //nolint:wrapcheck // transparent pass-through to the caller's writer
}

func (d *detachableWriter) detach() {
	if d == nil {
		return
	}
	d.detached.Store(true)
}

// ctxReader stops feeding a command once ctx is done, so that the goroutine
// copying a caller's stdin does not outlive a command Wait has abandoned. It
// cannot interrupt a Read already in progress -- an io.Reader has no way to
// be cancelled -- so it stops before the next one instead.
type ctxReader struct {
	ctx    context.Context //nolint:containedctx // an io.Reader has nowhere else to carry it
	reader io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, fmt.Errorf("stop reading command input: %w", err)
	}
	return c.reader.Read(p) //nolint:wrapcheck // transparent pass-through to the caller's reader
}

// winrmShell and winrmCommand are the parts of *winrm.Shell and
// *winrm.Command that a command uses once it is running. They are
// interfaces so that Wait's context handling can be tested: the concrete
// types talk SOAP to a real host and have no test double.
type winrmShell interface {
	Close() error
}

type winrmCommand interface {
	Close() error
	Wait()
	ExitCode() int
}

type command struct {
	ctx    context.Context //nolint:containedctx // needed to bound Wait, which takes no context of its own
	sh     winrmShell
	cmd    winrmCommand
	stdout *detachableWriter
	stderr *detachableWriter
	wg     sync.WaitGroup
}

// abandon gives up on a command that outlived the caller's context. The
// goroutines copying its output run until the command is torn down, so they
// are detached from the caller's writers first. The teardown itself runs in
// the background, because closing the command and the shell are synchronous
// requests to a host that by this point is very likely not answering, and
// waiting for them here would put the caller back where it started. They go
// in separate goroutines so that a close which never returns cannot strand
// the other one.
func (c *command) abandon() error {
	c.stdout.detach()
	c.stderr.detach()
	go func() { _ = c.cmd.Close() }()
	go func() { _ = c.sh.Close() }()
	return fmt.Errorf("%w: command did not complete", c.ctx.Err())
}

// Wait blocks until the command finishes or ctx (the context StartProcess was
// called with) is done, whichever happens first.
//
// Without this, a command whose underlying WinRM connection has silently
// died -- for example the host rebooted mid-command and this shell was
// never actually torn down on this end -- blocks forever: neither
// c.wg.Wait() nor the underlying masterzen/winrm Command.Wait() it calls
// take a context or have any internal timeout of their own. If ctx has no
// deadline this preserves the previous unbounded behaviour; callers that
// want a bound must set one on the context passed to StartProcess.
//
// Returning on ctx does not stop the remote command, and it is the one case
// where Wait returns while work is still going on: see StartProcess for what
// a caller may assume about its writers afterwards.
func (c *command) Wait() error {
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.wg.Wait()
		log.Trace(context.Background(), "waitgroup finished")
		c.cmd.Wait()
	}()

	select {
	case <-done:
	case <-c.ctx.Done():
		select {
		case <-done: // finished as the deadline passed: report the real result
		default:
			return c.abandon()
		}
	}

	defer c.sh.Close()
	defer c.cmd.Close()

	log.Trace(context.Background(), "command finished", log.KeyExitCode, c.cmd.ExitCode())

	if c.cmd.ExitCode() != 0 {
		return fmt.Errorf("%w: exit code %d", errExitCode, c.cmd.ExitCode())
	}

	return nil
}

// Close terminates the command.
func (c *command) Close() error {
	if err := c.cmd.Close(); err != nil {
		return fmt.Errorf("close command: %w", err)
	}
	return nil
}

// StartProcess executes a command on the remote host and uses the passed in streams for stdin, stdout and stderr. It returns a Waiter with a .Wait() function that
// blocks until the command finishes and returns an error if the exit code is not zero.
//
// If ctx is done first, Wait returns a context error instead of waiting for a
// command the host may never finish. The remote command is then torn down in
// the background, so on that path -- and only on that path -- Wait can return
// while the goroutines copying the command's output are still running. They
// are cut loose from stdout and stderr before Wait returns, but one write
// already in progress cannot be interrupted, because an io.Writer offers no
// way to do so and waiting for it would make Wait unbounded again. The same
// goes for stdin, which is read through a reader that stops at the next read
// boundary once ctx is done but cannot abandon a read already under way. A
// caller that recycles, closes or concurrently uses any of the three streams
// it passed in must therefore not do so when Wait reports a context error.
func (c *Connection) StartProcess(ctx context.Context, cmd string, stdin io.Reader, stdout, stderr io.Writer) (protocol.Waiter, error) {
	c.mu.Lock()
	client := c.client
	c.mu.Unlock()
	if client == nil {
		return nil, errNotConnected
	}
	if len(cmd) > 8191 {
		return nil, fmt.Errorf("%w: %w: command too long (%d/%d)", protocol.ErrNonRetryable, errInvalidCommand, len(cmd), 8191)
	}

	shell, err := client.CreateShell()
	if err != nil {
		return nil, fmt.Errorf("create shell: %w", err)
	}
	proc, err := shell.ExecuteWithContext(ctx, cmd)
	if err != nil {
		shell.Close()
		return nil, fmt.Errorf("execute command: %w", err)
	}
	started := time.Now()
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	res := &command{
		ctx:    ctx,
		sh:     shell,
		cmd:    proc,
		stdout: &detachableWriter{w: stdout},
		stderr: &detachableWriter{w: stderr},
	}
	if stdin == nil {
		proc.Stdin.Close()
	} else {
		res.wg.Go(func() {
			n, err := io.Copy(proc.Stdin, ctxReader{ctx: ctx, reader: stdin})
			if err != nil {
				log.Trace(ctx, "copying data to command stdin failed", log.KeyError, err)
				return
			}
			log.Trace(ctx, "finished copying data to command stdin", log.KeyBytes, n)
		})
	}
	res.wg.Add(2)
	go func() {
		defer res.wg.Done()
		n, err := io.Copy(res.stdout, proc.Stdout)
		if err != nil {
			log.Trace(ctx, "copying data from command stdout failed", log.KeyDuration, time.Since(started), log.KeyError, err)
			return
		}
		log.Trace(ctx, "finished copying data from stdout", log.KeyBytes, n)
	}()
	go func() {
		defer res.wg.Done()
		n, err := io.Copy(res.stderr, proc.Stderr)
		if err != nil {
			log.Trace(ctx, "copying data from command stderr failed", log.KeyDuration, time.Since(started), log.KeyError, err)
			return
		}
		log.Trace(ctx, "finished copying data from stderr", log.KeyBytes, n)
	}()
	return res, nil
}

// ExecInteractive executes a command on the host and passes stdin/stdout/stderr as-is to the session.
// The session is terminated when ctx is cancelled.
func (c *Connection) ExecInteractive(ctx context.Context, cmd string, stdin io.Reader, stdout, stderr io.Writer) error {
	c.mu.Lock()
	client := c.client
	c.mu.Unlock()
	if client == nil {
		return errNotConnected
	}
	if cmd == "" {
		cmd = "cmd.exe"
	}
	_, err := client.RunWithContextWithInput(ctx, cmd, stdout, stderr, stdin)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err() //nolint:wrapcheck // context error is the real cause
		}
		return fmt.Errorf("execute command in interactive mode: %w", err)
	}
	return nil
}
