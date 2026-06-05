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

	client  *ssh.Client
	bastion *Connection

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
	c.Log().Debug("building ssh config", "user", c.User, "host", c.Address)

	if c.Port != 0 && c.Port != 22 {
		c.sshConfig.Port = c.Port
		c.Log().Debug("propagating explicit port to ssh config", "port", c.Port)
	} else {
		c.Log().Debug("port is default (22) — deferring to ssh config / ~/.ssh/config", "port", c.Port)
	}

	if c.KeyPath != nil {
		c.sshConfig.IdentityFile = []string{*c.KeyPath}
		c.Log().Debug("propagating key path to ssh config", "key_path", *c.KeyPath)
	}

	if len(c.SSHConfigOptions) > 0 {
		c.Log().Debug("applying options to ssh config", "count", len(c.SSHConfigOptions))
		setter, err := sshconfig.NewSetter(c.sshConfig)
		if err != nil {
			return nil, fmt.Errorf("create sshconfig setter: %w", err)
		}
		setter.ErrorOnUnknownFields = true
		if err := c.SSHConfigOptions.ApplyTo(setter); err != nil {
			return nil, fmt.Errorf("%w: %w", protocol.ErrValidationFailed, err)
		}
	}

	if ConfigParser != nil {
		c.Log().Debug("applying ~/.ssh/config")
		if err := ConfigParser.Apply(c.sshConfig, c.Address); err != nil {
			return nil, fmt.Errorf("failed to apply ssh config: %w", err)
		}
	}

	if c.sshConfig.Port != 0 {
		c.Port = c.sshConfig.Port
	}
	c.Log().Debug("resolved final port", "port", c.Port)

	// If no explicit keepalive option was provided, honor ServerAliveInterval from the ssh config.
	// Note: platform-embedded defaults are included (e.g. macOS defaults to 30s), so a rig binary
	// built on macOS will enable keepalive for all connections unless explicitly overridden.
	// Pass WithKeepAlive(0) to disable keepalive regardless of the ssh config value.
	if options.KeepAliveInterval == nil && c.sshConfig.ServerAliveInterval > 0 {
		d := c.sshConfig.ServerAliveInterval
		options.KeepAliveInterval = &d
		c.Log().Debug("enabling keepalive from ssh config ServerAliveInterval", "interval", d)
	}

	return c, nil
}

// errSkipCache is a sentinel used by pkeySigner when the failure is conditional
// on per-connection state (BatchMode, PasswordCallback). clientConfig uses it to
// skip caching so the same key path can be retried by a connection with different
// settings in the same process.
var errSkipCache = errors.New("skip signer cache")

// skipCacheError wraps an error and marks it as non-cacheable via errSkipCache.
// Its Error() returns only the inner message so the sentinel text never appears
// in user-facing output while errors.Is(err, errSkipCache) still works.
type skipCacheError struct{ inner error }

func (e *skipCacheError) Error() string   { return e.inner.Error() }
func (e *skipCacheError) Unwrap() []error { return []error{errSkipCache, e.inner} }

func skipCache(err error) error { return &skipCacheError{inner: err} }

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
		if c.sshConfig.Port != 0 {
			c.Port = c.sshConfig.Port
		}

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

// Protocol returns the protocol family, "SSH".
func (c *Connection) Protocol() string {
	return "SSH"
}

// ProtocolName returns the implementation name, "SSH".
func (c *Connection) ProtocolName() string {
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
	if c.bastion != nil {
		c.bastion.Disconnect()
		c.bastion = nil
	}
}

// Disconnect closes the SSH connection.
func (c *Connection) Disconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.disconnect()
}

// detectWindows probes the remote host to determine whether it is running
// Windows and stores the result in the cache. The caller is responsible for
// ensuring the connection is established before calling this method.
// If called concurrently, the probe may run more than once, but the cached
// result is always consistent.
func (c *Connection) detectWindows(ctx context.Context) bool {
	c.mu.Lock()
	client := c.client
	c.mu.Unlock()

	if client == nil {
		return false
	}

	serverVersion := strings.ToLower(string(client.ServerVersion()))
	log.Trace(ctx, "checking if host is windows", "server_version", serverVersion)

	boolPtr := func(b bool) *bool { return &b }
	var isWin bool
	switch {
	case strings.Contains(serverVersion, "windows"):
		isWin = true
	case isKnownPosix(serverVersion):
		isWin = false
	default:
		isWinProc, err := c.StartProcess(ctx, "ver.exe", nil, nil, nil)
		isWin = err == nil && isWinProc.Wait() == nil
		// Don't cache a probe that failed due to context cancellation — the
		// result would be a false negative that persists for the lifetime of
		// the connection.
		if ctx.Err() != nil {
			return false
		}
	}

	log.Trace(ctx, fmt.Sprintf("host is windows: %t", isWin))

	c.mu.Lock()
	c.isWindows = boolPtr(isWin)
	c.mu.Unlock()

	return isWin
}

// IsWindows is true when the host is running windows.
// The result is cached after the first probe; subsequent calls are O(1).
// For reliable context propagation, Connect should be called first —
// detection is also triggered during Connect using the connect context.
func (c *Connection) IsWindows() bool {
	c.mu.Lock()
	if c.isWindows != nil {
		result := *c.isWindows
		c.mu.Unlock()
		return result
	}
	c.mu.Unlock()

	// Fallback: probe with a bounded background context.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return c.detectWindows(ctx)
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

// loadAgentSigners returns the signers offered by the local ssh agent and a
// closer for the agent connection. The caller must close the connection after
// the SSH handshake completes, because the signers rely on it for signing.
// Failures to reach the agent or list its signers are logged and result in a
// nil slice and a no-op closer rather than an error.
func (c *Connection) loadAgentSigners(ctx context.Context) ([]ssh.Signer, func()) {
	agentClient, closer, err := agent.NewClient()
	if err != nil {
		log.Trace(ctx, "failed to get ssh agent client", log.ErrorAttr(err))
		return nil, func() {}
	}
	closeAgent := func() {}
	if closer != nil {
		closeAgent = func() { _ = closer.Close() }
	}
	c.Log().Debug("using ssh agent")
	signers, err := agentClient.Signers()
	if err != nil {
		log.Trace(ctx, "failed to list signers from ssh agent", log.ErrorAttr(err))
		closeAgent()
		return nil, func() {}
	}
	return signers, closeAgent
}

// loadKeySigners loads signers for each configured key path, using signerCache
// to avoid re-parsing keys. Agent-backed signers (for .pub files or encrypted
// keys) are not cached because they require a live agent connection.
func (c *Connection) loadKeySigners(ctx context.Context, agentSigners []ssh.Signer) []ssh.Signer {
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
		signer, fromAgent, err := c.pkeySigner(ctx, agentSigners, keyPath)
		if err != nil {
			c.Log().Debug("failed to obtain a signer for identity", log.KeyFile, keyPath, log.ErrorAttr(err))
			if !errors.Is(err, errSkipCache) {
				signerCache.Store(keyPath, err)
			}
		} else {
			if !fromAgent {
				signerCache.Store(keyPath, signer)
			}
			keySigners = append(keySigners, signer)
		}
	}
	return keySigners
}

func applySSHConfigOptions(sshCfg *sshconfig.Config, config *ssh.ClientConfig) {
	if len(sshCfg.Ciphers) > 0 {
		config.Ciphers = sshCfg.Ciphers
	}
	if len(sshCfg.KexAlgorithms) > 0 {
		config.KeyExchanges = sshCfg.KexAlgorithms
	}
	if len(sshCfg.MACs) > 0 {
		config.MACs = sshCfg.MACs
	}
	if len(sshCfg.HostKeyAlgorithms) > 0 {
		config.HostKeyAlgorithms = sshCfg.HostKeyAlgorithms
	}
	if sshCfg.RekeyLimit.MaxData > 0 {
		config.RekeyThreshold = uint64(sshCfg.RekeyLimit.MaxData)
	}
}

func (c *Connection) clientConfig(ctx context.Context) (*ssh.ClientConfig, func(), error) {
	config := &ssh.ClientConfig{
		User: c.User,
	}

	hkc, err := c.hostkeyCallback(ctx)
	if err != nil {
		return nil, func() {}, err
	}
	config.HostKeyCallback = hkc

	applySSHConfigOptions(c.sshConfig, config)

	// PubkeyAuthentication is honored from the ssh config. When set to "no", all
	// public key authentication (ssh agent and identity files) is skipped.
	// PasswordAuthentication from ssh_config is not honored: rig does not read
	// passwords from config. Callers can still enable password auth by supplying
	// ssh.Password(...) via AuthMethods.
	pubkeyEnabled := !c.sshConfig.PubkeyAuthentication.IsFalse()

	var agentSigners []ssh.Signer
	agentClose := func() {}
	if pubkeyEnabled {
		agentSigners, agentClose = c.loadAgentSigners(ctx)
	} else {
		log.Trace(ctx, "public key authentication disabled by ssh config (PubkeyAuthentication no)")
	}

	if len(c.AuthMethods) > 0 {
		log.Trace(ctx, "using passed-in auth methods", "count", len(c.AuthMethods))
		config.Auth = c.AuthMethods
		return config, agentClose, nil
	}

	var keySigners []ssh.Signer
	if pubkeyEnabled {
		keySigners = c.loadKeySigners(ctx, agentSigners)
	}

	agentForAuth := agentSigners
	if c.sshConfig.IdentitiesOnly.IsTrue() {
		agentForAuth = nil
	}
	combined := mergeSigners(keySigners, agentForAuth)
	if len(combined) > 0 {
		c.Log().Debug("using public key authentication", "num_keys", len(combined))
		config.Auth = append(config.Auth, ssh.PublicKeys(combined...))
	}

	if len(config.Auth) == 0 {
		return nil, agentClose, fmt.Errorf("%w: no usable authentication method found", protocol.ErrNonRetryable)
	}

	return config, agentClose, nil
}

// dialWithDeadline opens a TCP channel through this connection to dst, honouring
// ctx cancellation and the given deadline. The goroutine running Dial is
// unblocked by disconnecting this connection when the deadline or ctx fires.
func (c *Connection) dialWithDeadline(ctx context.Context, deadline time.Time, dst string) (net.Conn, error) {
	dialCtx := ctx
	if !deadline.IsZero() {
		var cancel context.CancelFunc
		dialCtx, cancel = context.WithDeadline(ctx, deadline)
		defer cancel()
	}
	type result struct {
		conn net.Conn
		err  error
	}
	dialResult := make(chan result, 1)
	go func() {
		conn, err := c.Dial("tcp", dst)
		dialResult <- result{conn, err}
	}()
	select {
	case <-dialCtx.Done():
		ctxErr := fmt.Errorf("dial %s: %w", dst, dialCtx.Err())
		select {
		case r := <-dialResult:
			// Dial completed concurrently; discard the conn and honour cancellation.
			if r.conn != nil {
				r.conn.Close()
			}
		default:
			// Dial still in progress; disconnect to unblock c.Dial, then drain.
			c.Disconnect()
			go func() {
				if r := <-dialResult; r.conn != nil {
					r.conn.Close()
				}
			}()
		}
		return nil, ctxErr
	case r := <-dialResult:
		// Guard against the race where both cases become ready simultaneously.
		if err := dialCtx.Err(); err != nil {
			if r.conn != nil {
				r.conn.Close()
			}
			return nil, fmt.Errorf("dial %s: %w", dst, err)
		}
		return r.conn, r.err
	}
}

func (c *Connection) connectViaBastion(ctx context.Context, dst string, config *ssh.ClientConfig, agentClose func()) error {
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
	connected := false
	defer func() {
		if !connected {
			bastionSSH.Disconnect()
		}
	}()
	deadline := c.connectDeadline(ctx)
	bconn, err := bastionSSH.dialWithDeadline(ctx, deadline, dst)
	if err != nil {
		return fmt.Errorf("bastion dial: %w", err)
	}
	if !deadline.IsZero() {
		_ = bconn.SetDeadline(deadline)
	}
	client, chans, reqs, err := ssh.NewClientConn(bconn, dst, config)
	agentClose()
	if err != nil {
		_ = bconn.Close()
		if errors.Is(err, hostkey.ErrHostKeyMismatch) {
			return fmt.Errorf("%w: bastion client connect: %w", protocol.ErrNonRetryable, err)
		}
		return fmt.Errorf("bastion client connect: %w", err)
	}
	if !deadline.IsZero() {
		_ = bconn.SetDeadline(time.Time{})
	}
	connected = true
	c.mu.Lock()
	c.client = ssh.NewClient(client, chans, reqs)
	c.bastion = bastionSSH
	c.startKeepalive()
	c.mu.Unlock()

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

// startKeepalive starts the keepalive goroutine. Caller must hold c.mu.
func (c *Connection) startKeepalive() {
	if c.options.KeepAliveInterval == nil || *c.options.KeepAliveInterval <= 0 {
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

// dialNetwork returns the tcp network string based on the configured AddressFamily.
func (c *Connection) dialNetwork() string {
	switch c.sshConfig.AddressFamily {
	case "inet":
		return "tcp4"
	case "inet6":
		return "tcp6"
	default:
		return "tcp"
	}
}

// connectDeadline returns the earliest of the context deadline and the configured
// ConnectTimeout, or zero if neither is set.
func (c *Connection) connectDeadline(ctx context.Context) time.Time {
	var deadline time.Time
	if c.sshConfig.ConnectTimeout > 0 {
		deadline = time.Now().Add(c.sshConfig.ConnectTimeout)
	}
	if d, ok := ctx.Deadline(); ok && (deadline.IsZero() || d.Before(deadline)) {
		deadline = d
	}
	return deadline
}

// Connect opens the SSH connection.
func (c *Connection) Connect(ctx context.Context) error {
	c.mu.Lock()
	c.disconnect()
	c.mu.Unlock()

	c.SetDefaults(ctx)

	config, rawClose, err := c.clientConfig(ctx)
	var once sync.Once
	agentClose := func() { once.Do(rawClose) }
	defer agentClose()
	if err != nil {
		return fmt.Errorf("%w: create config: %w", protocol.ErrNonRetryable, err)
	}

	dst := net.JoinHostPort(c.Address, strconv.Itoa(c.Port))

	if c.Bastion != nil {
		return c.connectViaBastion(ctx, dst, config, agentClose)
	}

	deadline := c.connectDeadline(ctx)
	conn, err := (&net.Dialer{Deadline: deadline}).DialContext(ctx, c.dialNetwork(), dst)
	if err != nil {
		return fmt.Errorf("ssh dial: %w", err)
	}
	if !deadline.IsZero() {
		_ = conn.SetDeadline(deadline)
	}
	ncc, chans, reqs, err := ssh.NewClientConn(conn, dst, config)
	agentClose()
	if err != nil {
		_ = conn.Close()
		if errors.Is(err, hostkey.ErrHostKeyMismatch) {
			return fmt.Errorf("%w: %w", protocol.ErrNonRetryable, err)
		}
		return fmt.Errorf("ssh dial: %w", err)
	}
	_ = conn.SetDeadline(time.Time{})
	c.mu.Lock()
	c.client = ssh.NewClient(ncc, chans, reqs)
	c.startKeepalive()
	c.mu.Unlock()

	c.prewarmWindows(ctx)

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

// pkeySigner returns a signer for the key at path. The second return value is
// true when the signer is agent-backed and must not be stored in signerCache.
func (c *Connection) pkeySigner(ctx context.Context, agentSigners []ssh.Signer, path string) (ssh.Signer, bool, error) {
	path, err := homedir.ExpandFile(path)
	if err != nil {
		return nil, false, fmt.Errorf("expand keyfile path: %w", err)
	}
	log.Trace(ctx, "checking identity file", log.KeyFile, path)
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, false, fmt.Errorf("%w: read identity file %s: %w", protocol.ErrNonRetryable, path, err)
	}

	pubKey, _, _, _, err := ssh.ParseAuthorizedKey(key)
	if err == nil {
		log.Trace(ctx, "file is a public key", log.KeyFile, path)
		s, err := c.pubkeySigner(agentSigners, pubKey)
		return s, err == nil, err
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err == nil {
		c.Log().Debug("using an unencrypted private key", log.KeyFile, path)
		return signer, false, nil
	}

	if errors.As(err, new(*ssh.PassphraseMissingError)) { //nolint:nestif
		c.Log().Debug("key is encrypted", log.KeyFile, path)

		if len(agentSigners) > 0 {
			if signer, _, err := c.pkeySigner(ctx, agentSigners, path+".pub"); err == nil {
				return signer, true, nil
			}
		}

		if c.sshConfig.BatchMode.IsTrue() {
			return nil, false, skipCache(fmt.Errorf("%w: batch mode enabled: skipping encrypted key %s", protocol.ErrNonRetryable, path))
		}

		if c.PasswordCallback != nil {
			log.Trace(ctx, "asking for a password to decrypt key", log.HostAttr(c), log.KeyFile, path)
			pass, err := c.PasswordCallback()
			if err != nil {
				return nil, false, skipCache(fmt.Errorf("%w: failed to get password: %w", protocol.ErrNonRetryable, err))
			}
			signer, err := ssh.ParsePrivateKeyWithPassphrase(key, []byte(pass))
			if err != nil {
				return nil, false, skipCache(fmt.Errorf("%w: encrypted key %s decoding failed: %w", protocol.ErrNonRetryable, path, err))
			}
			return signer, false, nil
		}
		return nil, false, skipCache(fmt.Errorf("%w: encrypted key %s: no password callback", protocol.ErrNonRetryable, path))
	}

	return nil, false, fmt.Errorf("%w: can't parse keyfile: %s: %w", protocol.ErrNonRetryable, path, err)
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

// setupInteractivePTY configures a PTY on the session for a file-backed stdin
// and returns the raw-mode restore function. It sets the local terminal to raw
// mode so that keystrokes are forwarded unmodified.
func setupInteractivePTY(session *ssh.Session, inF *os.File) (func(), error) {
	stdinFD := int(inF.Fd())
	old, err := term.MakeRaw(stdinFD)
	if err != nil {
		return nil, fmt.Errorf("make local terminal raw: %w", err)
	}

	width, height, err := term.GetSize(stdinFD)
	if err != nil {
		_ = term.Restore(stdinFD, old)
		return nil, fmt.Errorf("get terminal size: %w", err)
	}

	modes := ssh.TerminalModes{ssh.ECHO: 1}
	if err := session.RequestPty("xterm", height, width, modes); err != nil {
		_ = term.Restore(stdinFD, old)
		return nil, fmt.Errorf("request pty: %w", err)
	}

	return func() { _ = term.Restore(stdinFD, old) }, nil
}

// prepareSessionInput wires stdin to the session. If stdin is a terminal
// *os.File a PTY is requested and a raw-mode restore function is returned;
// otherwise the reader is used as-is and the restore is a no-op.
func prepareSessionInput(session *ssh.Session, stdin io.Reader) (input io.Reader, restore func(), err error) {
	restore = func() {}
	inF, ok := stdin.(*os.File)
	if !ok {
		return stdin, restore, nil
	}
	if term.IsTerminal(int(inF.Fd())) {
		restore, err = setupInteractivePTY(session, inF)
		if err != nil {
			return nil, nil, err
		}
	}
	return inF, restore, nil
}

// defaultInteractiveStreams replaces nil streams with the process's standard
// streams so that callers can safely pass nil for any stream they don't need
// to redirect.
func defaultInteractiveStreams(stdin io.Reader, stdout, stderr io.Writer) (io.Reader, io.Writer, io.Writer) {
	if stdin == nil {
		stdin = os.Stdin
	}
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	return stdin, stdout, stderr
}

// ExecInteractive executes a command on the host and passes stdin/stdout/stderr as-is to the session.
// The session is closed when ctx is cancelled. Nil streams default to os.Stdin/os.Stdout/os.Stderr.
func (c *Connection) ExecInteractive(ctx context.Context, cmd string, stdin io.Reader, stdout, stderr io.Writer) error {
	stdin, stdout, stderr = defaultInteractiveStreams(stdin, stdout, stderr)
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

	// Close the session when the context is done, but also stop watching when
	// the function returns normally so that the goroutine does not leak.
	watchDone := make(chan struct{})
	defer close(watchDone)
	go func() {
		select {
		case <-ctx.Done():
			_ = session.Close()
		case <-watchDone:
		}
	}()

	session.Stdout = stdout
	session.Stderr = stderr

	input, restoreTerm, err := prepareSessionInput(session, stdin)
	if err != nil {
		return err
	}
	defer restoreTerm()

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
		if ctx.Err() != nil {
			return ctx.Err() //nolint:wrapcheck // context error is the real cause
		}
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
	pass, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("failed to read password: %w", err)
	}
	return string(pass), nil
}
