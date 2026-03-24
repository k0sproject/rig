// Package ssh provides a rig protocol implementation for SSH connections.
package ssh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/k0sproject/rig/v2/homedir"
	"github.com/k0sproject/rig/v2/log"
	"github.com/k0sproject/rig/v2/protocol"
	"github.com/k0sproject/rig/v2/protocol/ssh/agent"
	"github.com/k0sproject/rig/v2/protocol/ssh/hostkey"
	"github.com/k0sproject/rig/v2/sshconfig"
	ssh "golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

var errNotConnected = errors.New("not connected")

// Connection describes an SSH connection.
type Connection struct {
	log.LoggerInjectable `yaml:"-"`
	Config               `yaml:",inline"`

	sshConfig *sshconfig.Config

	options *Options

	alias string
	name  string

	isWindows *bool
	once      sync.Once
	mu        sync.Mutex

	client *ssh.Client

	done chan struct{}

	keyPaths []string
}

// NewConnection creates a new SSH connection. Error is currently always nil.
func NewConnection(cfg Config, opts ...Option) (*Connection, error) {
	options := NewOptions(opts...)
	options.InjectLoggerTo(cfg, log.KeyProtocol, "ssh-config")
	cfg.SetDefaults()

	c := &Connection{Config: cfg, options: options} //nolint:varnamelen
	options.InjectLoggerTo(c, log.KeyProtocol, "ssh")
	c.sshConfig = &sshconfig.Config{
		User: c.User,
		Host: c.Address,
	}

	if c.Port != 0 && c.Port != 22 {
		c.sshConfig.Port = c.Port
	}

	if c.KeyPath != nil {
		c.sshConfig.IdentityFile = []string{*c.KeyPath}
	}

	if ConfigParser != nil {
		if err := ConfigParser.Apply(c.sshConfig, c.Address); err != nil {
			return nil, fmt.Errorf("failed to apply ssh config: %w", err)
		}
	}

	c.Port = c.sshConfig.Port

	return c, nil
}

var (
	signerCache = sync.Map{}

	knownHostsMU sync.Mutex
	globalOnce   sync.Once

	// ErrChecksumMismatch is returned when the checksum of an uploaded file does not match expectation.
	ErrChecksumMismatch = errors.New("checksum mismatch")
)

// TODO make the parser initialization more elegant.
func init() {
	globalOnce.Do(func() {
		parser, err := sshconfig.NewParser(nil)
		if err == nil {
			ConfigParser = parser
		}
	})
}

// Dial initiates a connection to the addr from the remote host.
func (c *Connection) Dial(network, address string) (net.Conn, error) {
	c.mu.Lock()
	client := c.client
	c.mu.Unlock()
	if client == nil {
		return nil, errNotConnected
	}
	conn, err := client.Dial(network, address)
	if err != nil {
		return nil, fmt.Errorf("ssh dial: %w", err)
	}
	return conn, nil
}

func (c *Connection) keypathsFromConfig(ctx context.Context) []string {
	log.Trace(ctx, "trying to get a keyfile path from ssh config", log.KeyHost, c)
	idf := slices.Compact(c.sshConfig.IdentityFile)

	if len(idf) > 0 {
		log.Trace(ctx, fmt.Sprintf("detected %d identity file paths from ssh config", len(idf)), log.KeyFile, idf)
		return idf
	}
	log.Trace(ctx, "no identity file paths found in ssh config")
	return []string{}
}

// SetDefaults sets various default values.
func (c *Connection) SetDefaults(ctx context.Context) {
	c.once.Do(func() {
		c.Port = c.sshConfig.Port

		if c.sshConfig.Hostname != "" {
			c.alias = c.Address
			c.Address = c.sshConfig.Hostname
		}

		for _, p := range c.keypathsFromConfig(ctx) {
			expanded, err := homedir.ExpandFile(p)
			if err != nil {
				log.Trace(ctx, "expand and validate", log.KeyFile, p, log.KeyError, err)
				continue
			}
			c.Log().Debug("using identity file", log.KeyFile, expanded)
			c.keyPaths = append(c.keyPaths, expanded)
		}
	})
}

// Protocol returns the protocol name, "SSH".
func (c *Connection) Protocol() string {
	return "SSH"
}

// IPAddress returns the connection address.
func (c *Connection) IPAddress() string {
	return c.Address
}

// IsConnected returns true if the connection is open.
func (c *Connection) IsConnected() bool {
	c.mu.Lock()
	client := c.client
	c.mu.Unlock()
	if client == nil || client.Conn == nil {
		return false
	}
	_, _, err := client.SendRequest("keepalive@rig", true, nil)
	return err == nil
}

// ConfigParser is an instance of rig/v2/sshconfig.Parser - it is exported here for weird design decisions made in rig v0.x and will be removed in rig v2 final.
var ConfigParser *sshconfig.Parser

// String returns the connection's printable name.
func (c *Connection) String() string {
	if c.name == "" {
		c.name = net.JoinHostPort(c.Address, strconv.Itoa(c.Port))
	}

	return c.name
}

// disconnect performs the actual disconnect. Caller must hold c.mu or ensure
// single-threaded access (e.g. during initial connect/disconnect lifecycle).
func (c *Connection) disconnect() {
	if c.client == nil {
		return
	}
	if c.options.KeepAliveInterval != nil && c.done != nil {
		close(c.done)
		c.done = nil
	}
	c.client.Close()
	c.client = nil
}

// Disconnect closes the SSH connection.
func (c *Connection) Disconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.disconnect()
}

// IsWindows is true when the host is running windows.
func (c *Connection) IsWindows() bool {
	c.mu.Lock()
	if c.isWindows != nil {
		result := *c.isWindows
		c.mu.Unlock()
		return result
	}
	client := c.client
	c.mu.Unlock()

	if client == nil {
		return false
	}

	serverVersion := strings.ToLower(string(client.ServerVersion()))
	log.Trace(context.Background(), "checking if host is windows", "server_version", serverVersion)

	boolPtr := func(b bool) *bool { return &b }
	var isWin bool
	switch {
	case strings.Contains(serverVersion, "windows"):
		isWin = true
	case isKnownPosix(serverVersion):
		isWin = false
	default:
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		isWinProc, err := c.StartProcess(ctx, "ver.exe", nil, nil, nil)
		isWin = err == nil && isWinProc.Wait() == nil
	}

	log.Trace(context.Background(), fmt.Sprintf("host is windows: %t", isWin))

	c.mu.Lock()
	c.isWindows = boolPtr(isWin)
	c.mu.Unlock()

	return isWin
}

func knownhostsCallback(path string, permissive, hash bool) (ssh.HostKeyCallback, error) {
	cb, err := hostkey.KnownHostsFileCallback(path, permissive, hash)
	if err != nil {
		return nil, fmt.Errorf("%w: create host key validator: %w", protocol.ErrNonRetryable, err)
	}
	return cb, nil
}

func isPermissive(ctx context.Context, c *Connection) bool {
	if c.sshConfig.StrictHostKeyChecking.IsFalse() {
		log.Trace(ctx, "config StrictHostkeyChecking is set to 'no'", log.KeyHost, c)
		return true
	}

	return false
}

func shouldHash(ctx context.Context, c *Connection) bool {
	if c.sshConfig.HashKnownHosts.IsTrue() {
		log.Trace(ctx, "config HashKnownHosts is set", log.KeyHost, c)
		return true
	}
	return false
}

func (c *Connection) hostkeyCallback(ctx context.Context) (ssh.HostKeyCallback, error) {
	knownHostsMU.Lock()
	defer knownHostsMU.Unlock()

	permissive := isPermissive(ctx, c)
	hash := shouldHash(ctx, c)

	if path, ok := hostkey.KnownHostsPathFromEnv(); ok {
		if path == "" {
			return hostkey.InsecureIgnoreHostKeyCallback, nil
		}
		c.Log().Debug("using known_hosts file from SSH_KNOWN_HOSTS", log.KeyHost, c, log.KeyFile, path)
		return knownhostsCallback(path, permissive, hash)
	}

	var khPath string

	for _, f := range c.sshConfig.UserKnownHostsFile {
		log.Trace(ctx, "trying known_hosts file from ssh config", log.KeyHost, c, log.KeyFile, f)
		exp, err := homedir.Expand(f)
		if err == nil {
			khPath = exp
			break
		}
	}

	if khPath != "" {
		log.Trace(ctx, "using known_hosts file", log.KeyHost, c, log.KeyFile, khPath)
		return knownhostsCallback(khPath, permissive, hash)
	}

	return nil, fmt.Errorf("%w: no known_hosts file found", protocol.ErrNonRetryable)
}

// mergeSigners combines key-file signers and agent signers, deduplicating by
// public key bytes. Key-file signers take priority (appear first).
func mergeSigners(keySigners, agentSigners []ssh.Signer) []ssh.Signer {
	seen := make(map[string]struct{}, len(keySigners))
	out := make([]ssh.Signer, 0, len(keySigners)+len(agentSigners))
	for _, s := range keySigners {
		k := string(s.PublicKey().Marshal())
		if _, dup := seen[k]; !dup {
			seen[k] = struct{}{}
			out = append(out, s)
		}
	}
	for _, s := range agentSigners {
		k := string(s.PublicKey().Marshal())
		if _, dup := seen[k]; !dup {
			seen[k] = struct{}{}
			out = append(out, s)
		}
	}
	return out
}

func (c *Connection) clientConfig(ctx context.Context) (*ssh.ClientConfig, error) { //nolint:cyclop
	config := &ssh.ClientConfig{
		User: c.User,
	}

	hkc, err := c.hostkeyCallback(ctx)
	if err != nil {
		return nil, err
	}
	config.HostKeyCallback = hkc

	var agentSigners []ssh.Signer
	agentClient, err := agent.NewClient()
	if err != nil {
		log.Trace(ctx, "failed to get ssh agent client", log.ErrorAttr(err))
	} else {
		c.Log().Debug("using ssh agent")
		agentSigners, err = agentClient.Signers()
		if err != nil {
			log.Trace(ctx, "failed to list signers from ssh agent", log.ErrorAttr(err))
		}
	}

	if len(c.AuthMethods) > 0 {
		log.Trace(ctx, "using passed-in auth methods", "count", len(c.AuthMethods))
		config.Auth = c.AuthMethods
		return config, nil
	}

	var keySigners []ssh.Signer
	for _, keyPath := range c.keyPaths {
		keyPath, err := homedir.Expand(keyPath)
		if err != nil {
			log.Trace(ctx, "expand keypath", log.FileAttr(keyPath), log.ErrorAttr(err))
			continue
		}
		if cached, ok := signerCache.Load(keyPath); ok {
			switch v := cached.(type) {
			case ssh.Signer:
				log.Trace(ctx, "using cached signer", log.FileAttr(keyPath))
				keySigners = append(keySigners, v)
			case error:
				log.Trace(ctx, "already discarded key", log.FileAttr(keyPath), log.ErrorAttr(v))
			default:
				log.Trace(ctx, fmt.Sprintf("unexpected type %T for cached signer for %s", cached, keyPath))
			}
			continue
		}
		signer, err := c.pkeySigner(ctx, agentSigners, keyPath)
		if err != nil {
			c.Log().Debug("failed to obtain a signer for identity", log.KeyFile, keyPath, log.ErrorAttr(err))
			signerCache.Store(keyPath, err)
		} else {
			signerCache.Store(keyPath, signer)
			keySigners = append(keySigners, signer)
		}
	}

	combined := mergeSigners(keySigners, agentSigners)
	if len(combined) > 0 {
		c.Log().Debug("using public key authentication", "num_keys", len(combined))
		config.Auth = append(config.Auth, ssh.PublicKeys(combined...))
	}

	if len(config.Auth) == 0 {
		return nil, fmt.Errorf("%w: no usable authentication method found", protocol.ErrNonRetryable)
	}

	return config, nil
}

func (c *Connection) connectViaBastion(ctx context.Context, dst string, config *ssh.ClientConfig) error {
	bastion, err := c.Bastion.Connection() //nolint:contextcheck
	if err != nil {
		return fmt.Errorf("create bastion connection: %w", err)
	}
	bastionSSH, ok := bastion.(*Connection)
	if !ok {
		return fmt.Errorf("%w: bastion connection is not an SSH connection", protocol.ErrNonRetryable)
	}
	c.Log().Debug("connecting to bastion", log.HostAttr(c), "bastion", bastionSSH)
	if err := bastionSSH.Connect(ctx); err != nil {
		if errors.Is(err, hostkey.ErrHostKeyMismatch) {
			return fmt.Errorf("%w: bastion connect: %w", protocol.ErrNonRetryable, err)
		}
		return err
	}
	bconn, err := bastionSSH.Dial("tcp", dst)
	if err != nil {
		return fmt.Errorf("bastion dial: %w", err)
	}
	client, chans, reqs, err := ssh.NewClientConn(bconn, dst, config)
	if err != nil {
		_ = bconn.Close()
		if errors.Is(err, hostkey.ErrHostKeyMismatch) {
			return fmt.Errorf("%w: bastion client connect: %w", protocol.ErrNonRetryable, err)
		}
		return fmt.Errorf("bastion client connect: %w", err)
	}
	c.mu.Lock()
	c.client = ssh.NewClient(client, chans, reqs)
	c.startKeepalive()
	c.mu.Unlock()

	return nil
}

// startKeepalive starts the keepalive goroutine. Caller must hold c.mu.
func (c *Connection) startKeepalive() {
	if c.options.KeepAliveInterval == nil {
		return
	}

	done := make(chan struct{})
	c.done = done
	go func() {
		ticker := time.NewTicker(*c.options.KeepAliveInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if !c.IsConnected() {
					return
				}
			case <-done:
				return
			}
		}
	}()
}

// Connect opens the SSH connection.
func (c *Connection) Connect(ctx context.Context) error {
	c.SetDefaults(ctx)

	config, err := c.clientConfig(ctx)
	if err != nil {
		return fmt.Errorf("%w: create config: %w", protocol.ErrNonRetryable, err)
	}

	dst := net.JoinHostPort(c.Address, strconv.Itoa(c.Port))

	if c.Bastion != nil {
		return c.connectViaBastion(ctx, dst, config)
	}

	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", dst)
	if err != nil {
		return fmt.Errorf("ssh dial: %w", err)
	}
	ncc, chans, reqs, err := ssh.NewClientConn(conn, dst, config)
	if err != nil {
		_ = conn.Close()
		if errors.Is(err, hostkey.ErrHostKeyMismatch) {
			return fmt.Errorf("%w: %w", protocol.ErrNonRetryable, err)
		}
		return fmt.Errorf("ssh dial: %w", err)
	}
	c.mu.Lock()
	c.client = ssh.NewClient(ncc, chans, reqs)
	c.startKeepalive()
	c.mu.Unlock()

	return nil
}

func (c *Connection) pubkeySigner(agentSigners []ssh.Signer, key ssh.PublicKey) (ssh.Signer, error) {
	if len(agentSigners) == 0 {
		return nil, fmt.Errorf("%w: signer not found for public key", protocol.ErrNonRetryable)
	}

	for _, s := range agentSigners {
		if bytes.Equal(key.Marshal(), s.PublicKey().Marshal()) {
			c.Log().Debug("signer for public key available in ssh agent")
			return s, nil
		}
	}

	return nil, fmt.Errorf("%w: the provided key is a public key and is not known by agent", protocol.ErrNonRetryable)
}

func (c *Connection) pkeySigner(ctx context.Context, agentSigners []ssh.Signer, path string) (ssh.Signer, error) {
	path, err := homedir.ExpandFile(path)
	if err != nil {
		return nil, fmt.Errorf("expand keyfile path: %w", err)
	}
	log.Trace(ctx, "checking identity file", log.KeyFile, path)
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: read identity file %s: %w", protocol.ErrNonRetryable, path, err)
	}

	pubKey, _, _, _, err := ssh.ParseAuthorizedKey(key)
	if err == nil {
		log.Trace(ctx, "file is a public key", log.KeyFile, path)
		return c.pubkeySigner(agentSigners, pubKey)
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err == nil {
		c.Log().Debug("using an unencrypted private key", log.KeyFile, path)
		return signer, nil
	}

	if errors.As(err, new(*ssh.PassphraseMissingError)) { //nolint:nestif
		c.Log().Debug("key is encrypted", log.KeyFile, path)

		if len(agentSigners) > 0 {
			if signer, err := c.pkeySigner(ctx, agentSigners, path+".pub"); err == nil {
				return signer, nil
			}
		}

		if c.PasswordCallback != nil {
			log.Trace(ctx, "asking for a password to decrypt key", log.HostAttr(c), log.KeyFile, path)
			pass, err := c.PasswordCallback()
			if err != nil {
				return nil, fmt.Errorf("%w: failed to get password: %w", protocol.ErrNonRetryable, err)
			}
			signer, err := ssh.ParsePrivateKeyWithPassphrase(key, []byte(pass))
			if err != nil {
				return nil, fmt.Errorf("%w: encrypted key %s decoding failed: %w", protocol.ErrNonRetryable, path, err)
			}
			return signer, nil
		}
	}

	return nil, fmt.Errorf("%w: can't parse keyfile: %s: %w", protocol.ErrNonRetryable, path, err)
}

// StartProcess executes a command on the remote host and uses the passed in streams for stdin, stdout and stderr. It returns a Waiter with a .Wait() function that
// blocks until the command finishes and returns an error if the exit code is not zero.
func (c *Connection) StartProcess(ctx context.Context, cmd string, stdin io.Reader, stdout, stderr io.Writer) (protocol.Waiter, error) {
	c.mu.Lock()
	client := c.client
	c.mu.Unlock()

	if client == nil {
		return nil, errNotConnected
	}

	session, err := client.NewSession()
	if err != nil {
		log.Trace(ctx, "ssh session creation failed, attempting reconnect", log.HostAttr(c), log.KeyError, err)
		c.mu.Lock()
		c.disconnect()
		c.mu.Unlock()
		reconnErr := c.Connect(ctx)
		if reconnErr != nil {
			return nil, fmt.Errorf("reconnect after session creation failure: %w", reconnErr)
		}
		c.mu.Lock()
		client = c.client
		c.mu.Unlock()
		if client == nil {
			return nil, errNotConnected
		}
		session, err = client.NewSession()
		if err != nil {
			return nil, fmt.Errorf("create ssh session: %w", err)
		}
	}

	session.Stdin = stdin
	session.Stdout = stdout
	session.Stderr = stderr

	go func() {
		<-ctx.Done()
		if ctx.Err() != nil {
			_ = session.Signal(ssh.SIGINT)
			_ = session.Close()
		}
	}()

	if err := session.Start(cmd); err != nil {
		return nil, fmt.Errorf("start session: %w", err)
	}

	return session, nil
}

// ExecInteractive executes a command on the host and passes stdin/stdout/stderr as-is to the session.
func (c *Connection) ExecInteractive(cmd string, stdin io.Reader, stdout, stderr io.Writer) error {
	c.mu.Lock()
	client := c.client
	c.mu.Unlock()
	if client == nil {
		return errNotConnected
	}
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("ssh new session: %w", err)
	}
	defer session.Close()

	session.Stdout = stdout
	session.Stderr = stderr
	var input io.Reader

	if inF, ok := stdin.(*os.File); ok {
		fd := int(os.Stdin.Fd()) //nolint:gosec // G115: os.Stdin.Fd() always returns valid int
		old, err := term.MakeRaw(fd)
		if err != nil {
			return fmt.Errorf("make local terminal raw: %w", err)
		}

		defer func(fd int, old *term.State) {
			_ = term.Restore(fd, old)
		}(fd, old)

		rows, cols, err := term.GetSize(fd)
		if err != nil {
			return fmt.Errorf("get terminal size: %w", err)
		}

		modes := ssh.TerminalModes{ssh.ECHO: 1}
		err = session.RequestPty("xterm", cols, rows, modes)
		if err != nil {
			return fmt.Errorf("request pty: %w", err)
		}
		input = inF
	} else {
		input = stdin
	}

	stdinpipe, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("get stdin pipe: %w", err)
	}
	go func() {
		_, _ = io.Copy(stdinpipe, input)
	}()

	cancel := captureSignals(stdinpipe, session)
	defer cancel()

	if cmd == "" {
		err = session.Shell()
	} else {
		err = session.Start(cmd)
	}

	if err != nil {
		return fmt.Errorf("start ssh session: %w", err)
	}

	if err := session.Wait(); err != nil {
		return fmt.Errorf("ssh session wait: %w", err)
	}

	return nil
}

// ParseSSHPrivateKey is a convenience utility to parses a private key and
// return []ssh.AuthMethod to be used in SSH{} AuthMethods field. This
// way you can avoid importing golang.org/x/crypto/ssh in your code
// and handle the passphrase prompt in a callback function.
func ParseSSHPrivateKey(key []byte, callback PasswordCallback) ([]ssh.AuthMethod, error) {
	signer, err := ssh.ParsePrivateKey(key)
	if err == nil {
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	}
	var ppErr *ssh.PassphraseMissingError
	if !errors.As(err, &ppErr) {
		return nil, fmt.Errorf("failed to parse key: %w", err)
	}
	if callback == nil {
		return nil, fmt.Errorf("key is encrypted and no callback provided: %w", err)
	}
	pass, err := callback()
	if err != nil {
		return nil, fmt.Errorf("failed to get passphrase: %w", err)
	}
	signer, err = ssh.ParsePrivateKeyWithPassphrase(key, []byte(pass))
	if err != nil {
		return nil, fmt.Errorf("failed to parse key with passphrase: %w", err)
	}
	return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
}

// DefaultPasswordCallback is a default implementation for PasswordCallback.
func DefaultPasswordCallback() (string, error) {
	fmt.Print("Enter passphrase: ")
	pass, err := term.ReadPassword(int(os.Stdin.Fd())) //nolint:gosec // G115: os.Stdin.Fd() always returns valid int
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("failed to read password: %w", err)
	}
	return string(pass), nil
}
