package ssh

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/k0sproject/rig/v2/protocol"
	"github.com/k0sproject/rig/v2/protocol/ssh/hostkey"
	ssh "golang.org/x/crypto/ssh"
)

var (
	errEmptyProxyJumpHost     = errors.New("empty host in ProxyJump entry")
	errProxyJumpPortRange     = errors.New("port out of range in ProxyJump entry")
	errProxyJumpTrailingColon = errors.New("trailing colon in ProxyJump address")
)

// proxyAddr is a net.Addr that carries a host:port string for the ProxyCommand
// transport. The knownhosts library calls SplitHostPort on RemoteAddr().String(),
// so it must look like a valid host:port, not an opaque label.
type proxyAddr struct{ hostport string }

func (a proxyAddr) Network() string { return "tcp" }
func (a proxyAddr) String() string  { return a.hostport }

// cmdConn wraps a subprocess's stdin/stdout as a net.Conn for use as an SSH transport.
// remote is the actual server address (host:port) used by the host-key callback.
// Deadline methods are no-ops; deadline enforcement is handled at the caller level via
// context cancellation + process kill.
type cmdConn struct {
	stdin  io.WriteCloser
	stdout io.ReadCloser
	remote proxyAddr
	once   sync.Once
}

func (c *cmdConn) Read(b []byte) (int, error)  { return c.stdout.Read(b) } //nolint:wrapcheck // io.Reader contract: callers compare against io.EOF, wrapping breaks it
func (c *cmdConn) Write(b []byte) (int, error) { return c.stdin.Write(b) } //nolint:wrapcheck // io.Writer contract: callers compare against io.EOF, wrapping breaks it
func (c *cmdConn) Close() error {
	c.once.Do(func() {
		_ = c.stdin.Close()
		_ = c.stdout.Close()
	})
	return nil
}

func (c *cmdConn) LocalAddr() net.Addr                { return proxyAddr{} }
func (c *cmdConn) RemoteAddr() net.Addr               { return c.remote }
func (c *cmdConn) SetDeadline(_ time.Time) error      { return nil }
func (c *cmdConn) SetReadDeadline(_ time.Time) error  { return nil }
func (c *cmdConn) SetWriteDeadline(_ time.Time) error { return nil }

// parseJumpPort parses and validates a port string from a ProxyJump address.
func parseJumpPort(portStr string) (int, error) {
	portNum, err := strconv.Atoi(portStr)
	if err != nil {
		return 0, fmt.Errorf("invalid port in ProxyJump %q: %w", portStr, err)
	}
	if portNum < 1 || portNum > 65535 {
		return 0, fmt.Errorf("%w: %d", errProxyJumpPortRange, portNum)
	}
	return portNum, nil
}

// stripBrackets removes surrounding [] from a bracketed IPv6 address string.
// Returns the string unchanged if it is not bracketed.
func stripBrackets(s string) string {
	if len(s) >= 2 && s[0] == '[' && s[len(s)-1] == ']' {
		return s[1 : len(s)-1]
	}
	return s
}

// parseProxyJump parses a single ProxyJump entry of the form
// [user@]host[:port] or ssh://[user@]host[:port] and returns a Config for it.
func parseProxyJump(jump, defaultUser string) (*Config, error) {
	jump = strings.TrimPrefix(jump, "ssh://")

	user := defaultUser
	if at := strings.LastIndex(jump, "@"); at >= 0 {
		user = jump[:at]
		jump = jump[at+1:]
	}
	if user == "" {
		user = "root"
	}

	port := 22
	host, portStr, splitErr := net.SplitHostPort(jump)
	if splitErr != nil {
		// A trailing colon (e.g. "example.com:") is always invalid.
		if strings.HasSuffix(jump, ":") {
			return nil, fmt.Errorf("%w: %q", errProxyJumpTrailingColon, jump)
		}
		// Detect the no-port case by structure rather than matching the error string
		// (which is not part of the net package's API contract).
		// A valid no-port address is a plain hostname (no colon) or a properly
		// bracketed IPv6 like [::1] (starts with "[" AND ends with "]").
		// Reject unbracketed IPv6 (e.g. "::1") and malformed brackets (e.g. "[::1").
		if (strings.HasPrefix(jump, "[") && !strings.HasSuffix(jump, "]")) ||
			(!strings.HasPrefix(jump, "[") && strings.ContainsRune(jump, ':')) {
			return nil, fmt.Errorf("invalid ProxyJump address %q: %w", jump, splitErr)
		}
		// Bare bracketed IPv6 like [::1] must have brackets stripped so
		// Config.Address is a bare host suitable for net.JoinHostPort.
		host = stripBrackets(jump)
	} else {
		var err error
		if port, err = parseJumpPort(portStr); err != nil {
			return nil, err
		}
	}

	if host == "" {
		return nil, fmt.Errorf("%w: %q", errEmptyProxyJumpHost, jump)
	}

	return &Config{Address: host, User: user, Port: port}, nil
}

// proxyCommandClientConfig returns a copy of config with CheckHostIP disabled.
// Per ssh_config(5), CheckHostIP is unsupported for ProxyCommand: the transport
// (subprocess stdin/stdout) does not expose the real TCP peer address, so
// DNS-based IP verification would give incorrect results.
func (c *Connection) proxyCommandClientConfig(ctx context.Context, config *ssh.ClientConfig) (*ssh.ClientConfig, error) {
	if !c.sshConfig.CheckHostIP.IsTrue() || c.sshConfig.HostKeyAlias != "" {
		return config, nil
	}
	hkc, err := c.hostkeyCallback(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("%w: proxy host key callback: %w", protocol.ErrNonRetryable, err)
	}
	proxyCfg := *config
	proxyCfg.HostKeyCallback = hkc
	return &proxyCfg, nil
}

// startProxyProcess starts the ProxyCommand subprocess, wires its stdin/stdout
// as a net.Conn, and returns a kill function for cleanup. The returned killProc
// must be called if the caller does not store proc for later reaping.
func startProxyProcess(pcmd, dst string) (pconn *cmdConn, proc *exec.Cmd, killProc func(), err error) {
	args := proxyCommandArgs(pcmd)
	proc = exec.Command(args[0], args[1:]...) //nolint:noctx,gosec // proxy process lifetime is bound to the connection, not the connect ctx; ProxyCommand is from user ssh config
	proc.Stderr = os.Stderr                   // surface proxy process errors to the caller, matching OpenSSH behavior

	stdinPipe, err := proc.StdinPipe()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%w: proxy command stdin pipe: %w", protocol.ErrNonRetryable, err)
	}
	stdoutPipe, err := proc.StdoutPipe()
	if err != nil {
		_ = stdinPipe.Close()
		return nil, nil, nil, fmt.Errorf("%w: proxy command stdout pipe: %w", protocol.ErrNonRetryable, err)
	}
	if err := proc.Start(); err != nil {
		_ = stdinPipe.Close()
		_ = stdoutPipe.Close()
		return nil, nil, nil, fmt.Errorf("%w: proxy command start: %w", protocol.ErrNonRetryable, err)
	}
	pconn = &cmdConn{stdin: stdinPipe, stdout: stdoutPipe, remote: proxyAddr{hostport: dst}}
	killProc = buildKillFunc(proc)
	return pconn, proc, killProc, nil
}

// connectViaProxyCommand runs the configured ProxyCommand, wraps its stdin/stdout as
// an SSH transport, and completes the SSH handshake. The process is stored on c so
// that disconnect() can reap it.
func (c *Connection) connectViaProxyCommand(ctx context.Context, dst string, config *ssh.ClientConfig, agentClose func()) error {
	pcmd := c.sshConfig.ProxyCommand
	c.Log().Debug("connecting via ProxyCommand")

	var err error
	config, err = c.proxyCommandClientConfig(ctx, config)
	if err != nil {
		return err
	}

	pconn, proc, killProc, err := startProxyProcess(pcmd, dst)
	if err != nil {
		return err
	}

	// Run the SSH handshake in a goroutine so we can honour the connect deadline /
	// context cancellation (cmdConn.SetDeadline is a no-op).
	type connResult struct {
		ncc   ssh.Conn
		chans <-chan ssh.NewChannel
		reqs  <-chan *ssh.Request
		err   error
	}
	resultCh := make(chan connResult, 1)
	go func() {
		ncc, chans, reqs, hsErr := ssh.NewClientConn(pconn, dst, config)
		resultCh <- connResult{ncc, chans, reqs, hsErr}
	}()

	deadline := c.connectDeadline(ctx)
	dialCtx := ctx
	if !deadline.IsZero() {
		var cancel context.CancelFunc
		dialCtx, cancel = context.WithDeadline(ctx, deadline)
		defer cancel()
	}

	var result connResult
	select {
	case <-dialCtx.Done():
		_ = pconn.Close()
		killProc()
		// Try a non-blocking drain first: if the handshake goroutine already wrote
		// its result, close any ssh.Conn it produced and we're done.
		// If it hasn't finished yet, the pipe close and process kill above will
		// cause ssh.NewClientConn to return (pipe EOF / broken pipe), so the
		// goroutine will write to the buffered channel and exit without blocking.
		// The spawned goroutine closes any ssh.Conn that happened to complete just
		// as the context fired.
		select {
		case r := <-resultCh:
			if r.ncc != nil {
				_ = r.ncc.Close()
			}
		default:
			go func() {
				if r := <-resultCh; r.ncc != nil {
					_ = r.ncc.Close()
				}
			}()
		}
		agentClose()
		return fmt.Errorf("proxy command connect: %w", dialCtx.Err())
	case result = <-resultCh:
	}

	agentClose()

	if result.err != nil {
		_ = pconn.Close()
		killProc()
		if errors.Is(result.err, hostkey.ErrHostKeyMismatch) {
			return fmt.Errorf("%w: %w", protocol.ErrNonRetryable, result.err)
		}
		return fmt.Errorf("proxy command ssh connect: %w", result.err)
	}

	c.mu.Lock()
	c.client = ssh.NewClient(result.ncc, result.chans, result.reqs)
	c.proxyCmd = proc
	c.startKeepalive()
	c.mu.Unlock()

	c.prewarmWindows(ctx)

	return nil
}
