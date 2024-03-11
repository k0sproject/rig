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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/k0sproject/rig/v2/homedir"
	"github.com/k0sproject/rig/v2/log"
	"github.com/k0sproject/rig/v2/protocol"
	"github.com/k0sproject/rig/v2/protocol/ssh/agent"
	"github.com/k0sproject/rig/v2/protocol/ssh/hostkey"
	"github.com/kevinburke/ssh_config"
	"github.com/mattn/go-shellwords"
	ssh "golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

var errNotConnected = errors.New("not connected")

// Connection describes an SSH connection.
type Connection struct {
	log.LoggerInjectable `yaml:"-"`
	Config               `yaml:",inline"`

	options *Options

	alias string
	name  string

	isWindows *bool
	once      sync.Once

	client *ssh.Client

	done chan struct{}

	keyPaths []string
}

// NewConnection creates a new SSH connection. Error is currently always nil.
func NewConnection(cfg Config, opts ...Option) (*Connection, error) {
	options := NewOptions(opts...)
	options.InjectLoggerTo(cfg, log.KeyProtocol, "ssh-config")
	cfg.SetDefaults()

	c := &Connection{Config: cfg, options: options}
	options.InjectLoggerTo(c, log.KeyProtocol, "ssh")

	return c, nil
}

var (
	authMethodCache   = sync.Map{}
	defaultKeypaths   = []string{"~/.ssh/id_rsa", "~/.ssh/identity", "~/.ssh/id_dsa", "~/.ssh/id_ecdsa", "~/.ssh/id_ed25519"}
	dummyhostKeyPaths []string
	globalOnce        sync.Once
	knownHostsMU      sync.Mutex

	// ErrChecksumMismatch is returned when the checksum of an uploaded file does not match expectation.
	ErrChecksumMismatch = errors.New("checksum mismatch")
)

const hopefullyNonexistentHost = "thisH0stDoe5not3xist"

// Dial initiates a connection to the addr from the remote host.
func (c *Connection) Dial(network, address string) (net.Conn, error) {
	conn, err := c.client.Dial(network, address)
	if err != nil {
		return nil, fmt.Errorf("ssh dial: %w", err)
	}
	return conn, nil
}

func (c *Connection) keypathsFromConfig() []string {
	log.Trace(context.Background(), "trying to get a keyfile path from ssh config", log.KeyHost, c)
	idf := c.getConfigAll("IdentityFile")
	// https://github.com/kevinburke/ssh_config/blob/master/config.go#L254 says:
	// TODO: IdentityFile has multiple default values that we should return
	// To work around this, the hard coded list of known defaults are appended to the list
	idf = append(idf, defaultKeypaths...)
	sort.Strings(idf)
	idf = slices.Compact(idf)

	if len(idf) > 0 {
		log.Trace(context.Background(), fmt.Sprintf("detected %d identity file paths from ssh config", len(idf)), log.KeyFile, idf)
		return idf
	}
	log.Trace(context.Background(), "no identity file paths found in ssh config")
	return []string{}
}

func (c *Connection) initGlobalDefaults() {
	dummyHostIdentityFiles := SSHConfigGetAll(hopefullyNonexistentHost, "IdentityFile")
	// https://github.com/kevinburke/ssh_config/blob/master/config.go#L254 says:
	// TODO: IdentityFile has multiple default values that we should return
	// To work around this, the hard coded list of known defaults are appended to the list
	dummyHostIdentityFiles = append(dummyHostIdentityFiles, defaultKeypaths...)
	sort.Strings(dummyHostIdentityFiles)
	dummyHostIdentityFiles = slices.Compact(dummyHostIdentityFiles)
	for _, keyPath := range dummyHostIdentityFiles {
		if expanded, err := homedir.Expand(keyPath); err == nil {
			dummyhostKeyPaths = append(dummyhostKeyPaths, expanded)
		}
	}
}

func findUniq(a, b []string) (string, bool) {
	for _, s := range a {
		found := false
		for _, t := range b {
			if s == t {
				found = true
				break
			}
		}
		if !found {
			return s, true
		}
	}
	return "", false
}

// SetDefaults sets various default values.
func (c *Connection) SetDefaults() {
	globalOnce.Do(c.initGlobalDefaults)
	c.once.Do(func() {
		if c.KeyPath != nil && *c.KeyPath != "" {
			if expanded, err := homedir.Expand(*c.KeyPath); err == nil {
				c.keyPaths = append(c.keyPaths, expanded)
			}
			// keypath is explicitly set, accept the fact even if it's invalid and
			// don't try to find it from ssh config/defaults
			return
		}
		c.KeyPath = nil

		paths := c.keypathsFromConfig()

		if c.Port == 0 || c.Port == 22 {
			ports := c.getConfigAll("Port")
			if len(ports) > 0 {
				if p, err := strconv.Atoi(ports[0]); err == nil {
					c.Port = p
				}
			}
		}

		addrs := c.getConfigAll("HostName")
		if len(addrs) > 0 {
			c.alias = c.Address
			c.Address = addrs[0]
		}

		for _, p := range paths {
			expanded, err := homedir.ExpandFile(p)
			if err != nil {
				log.Trace(context.Background(), "expand and validate", log.KeyFile, p, log.KeyError, err)
				continue
			}
			c.Log().Debug("using identity file", log.KeyFile, expanded)
			c.keyPaths = append(c.keyPaths, expanded)
		}

		// check if all the paths that were found are global defaults
		// errors are handled differently when a keypath is explicitly set vs when it's defaulted
		if uniq, found := findUniq(c.keyPaths, dummyhostKeyPaths); found {
			c.KeyPath = &uniq
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
	if c.client == nil || c.client.Conn == nil {
		return false
	}
	_, _, err := c.client.Conn.SendRequest("keepalive@rig", true, nil)
	return err == nil
}

// SSHConfigGetAll by default points to ssh_config package's GetAll() function
// you can override it with your own implementation for testing purposes.
var SSHConfigGetAll = ssh_config.GetAll

func (c *Connection) getConfigAll(key string) []string {
	if c.alias != "" {
		return SSHConfigGetAll(c.alias, key)
	}
	return SSHConfigGetAll(c.Address, key)
}

// String returns the connection's printable name.
func (c *Connection) String() string {
	if c.name == "" {
		c.name = net.JoinHostPort(c.Address, strconv.Itoa(c.Port))
	}

	return c.name
}

// Disconnect closes the SSH connection.
func (c *Connection) Disconnect() {
	if c.client == nil {
		return
	}
	if c.options.KeepAliveInterval != nil {
		close(c.done)
	}
	c.client.Close()
}

func boolptr(b bool) *bool {
	return &b
}

// IsWindows is true when the host is running windows.
func (c *Connection) IsWindows() bool {
	if c.isWindows != nil {
		return *c.isWindows
	}

	if c.client == nil {
		return false
	}

	serverVersion := strings.ToLower(string(c.client.ServerVersion()))
	log.Trace(context.Background(), "checking if host is windows", "server_version", serverVersion)

	switch {
	case strings.Contains(serverVersion, "windows"):
		c.isWindows = boolptr(true)
	case isKnownPosix(serverVersion):
		c.isWindows = boolptr(false)
	default:
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		isWinProc, err := c.StartProcess(ctx, "ver.exe", nil, nil, nil)
		c.isWindows = boolptr(err == nil && isWinProc.Wait() == nil)
	}

	log.Trace(context.Background(), fmt.Sprintf("host is windows: %t", *c.isWindows))

	return *c.isWindows
}

func knownhostsCallback(path string, permissive, hash bool) (ssh.HostKeyCallback, error) {
	cb, err := hostkey.KnownHostsFileCallback(path, permissive, hash)
	if err != nil {
		return nil, fmt.Errorf("%w: create host key validator: %w", protocol.ErrAbort, err)
	}
	return cb, nil
}

func isPermissive(c *Connection) bool {
	if strict := c.getConfigAll("StrictHostkeyChecking"); len(strict) > 0 && strict[0] == "no" {
		log.Trace(context.Background(), "config StrictHostkeyChecking is set to 'no'", log.KeyHost, c)
		return true
	}

	return false
}

func shouldHash(c *Connection) bool {
	var hash bool
	if hashKnownHosts := c.getConfigAll("HashKnownHosts"); len(hashKnownHosts) == 1 {
		hash := hashKnownHosts[0] == "yes"
		if hash {
			log.Trace(context.Background(), "config HashKnownHosts is set", log.KeyHost, c)
		}
	}
	return hash
}

func (c *Connection) hostkeyCallback() (ssh.HostKeyCallback, error) {
	knownHostsMU.Lock()
	defer knownHostsMU.Unlock()

	permissive := isPermissive(c)
	hash := shouldHash(c)

	if path, ok := hostkey.KnownHostsPathFromEnv(); ok {
		if path == "" {
			return hostkey.InsecureIgnoreHostKeyCallback, nil
		}
		c.Log().Debug("using known_hosts file from SSH_KNOWN_HOSTS", log.KeyHost, c, log.KeyFile, path)
		return knownhostsCallback(path, permissive, hash)
	}

	var khPath string

	// Ask ssh_config for a known hosts file
	kfs := c.getConfigAll("UserKnownHostsFile")
	// splitting the result as for some reason ssh_config sometimes seems to
	// return a single string containing space separated paths
	if files, err := shellwords.Parse(strings.Join(kfs, " ")); err == nil {
		for _, f := range files {
			log.Trace(context.Background(), "trying known_hosts file from ssh config", log.KeyHost, c, log.KeyFile, f)
			exp, err := homedir.Expand(f)
			if err == nil {
				khPath = exp
				break
			}
		}
	}

	if khPath != "" {
		log.Trace(context.Background(), "using known_hosts file from ssh config", log.KeyHost, c, log.KeyFile, khPath)
		return knownhostsCallback(khPath, permissive, hash)
	}

	log.Trace(context.Background(), "using default known_hosts file", log.KeyHost, c, log.KeyFile, hostkey.DefaultKnownHostsPath)
	defaultPath, err := homedir.Expand(hostkey.DefaultKnownHostsPath)
	if err != nil {
		return nil, fmt.Errorf("expand known_hosts file path: %w", err)
	}

	return knownhostsCallback(defaultPath, permissive, hash)
}

func (c *Connection) clientConfig() (*ssh.ClientConfig, error) { //nolint:cyclop
	config := &ssh.ClientConfig{
		User: c.User,
	}

	hkc, err := c.hostkeyCallback()
	if err != nil {
		return nil, err
	}
	config.HostKeyCallback = hkc

	var signers []ssh.Signer
	agent, err := agent.NewClient()
	if err != nil {
		log.Trace(context.Background(), "failed to get ssh agent client", log.ErrorAttr(err))
	} else {
		c.Log().Debug("using ssh agent")
		signers, err = agent.Signers()
		if err != nil {
			log.Trace(context.Background(), "failed to list signers from ssh agent", log.ErrorAttr(err))
		}
	}

	if len(c.AuthMethods) > 0 {
		log.Trace(context.Background(), "using passed-in auth methods", "count", len(c.AuthMethods))
		config.Auth = c.AuthMethods
	} else if len(signers) > 0 {
		c.Log().Debug("using all keys from ssh agent because a keypath was not explicitly given", "count", len(signers))
		config.Auth = append(config.Auth, ssh.PublicKeys(signers...))
	}

	for _, keyPath := range c.keyPaths {
		keyPath, err := homedir.Expand(keyPath)
		if err != nil {
			log.Trace(context.Background(), "expand keypath", log.FileAttr(keyPath), log.ErrorAttr(err))
			continue
		}
		if am, ok := authMethodCache.Load(keyPath); ok {
			switch authM := am.(type) {
			case ssh.AuthMethod:
				log.Trace(context.Background(), "using cached auth method", log.FileAttr(keyPath))
				config.Auth = append(config.Auth, authM)
			case error:
				log.Trace(context.Background(), "already discarded key", log.FileAttr(keyPath), log.ErrorAttr(authM))
			default:
				log.Trace(context.Background(), fmt.Sprintf("unexpected type %T for cached auth method for %s", am, keyPath))
			}
			continue
		}
		privateKeyAuth, err := c.pkeySigner(signers, keyPath)
		if err != nil {
			c.Log().Debug("failed to obtain a signer for identity", log.KeyFile, keyPath, log.ErrorAttr(err))
			// store the error so this key won't be loaded again
			authMethodCache.Store(keyPath, err)
		} else {
			authMethodCache.Store(keyPath, privateKeyAuth)
			config.Auth = append(config.Auth, privateKeyAuth)
		}
	}

	if len(config.Auth) == 0 {
		return nil, fmt.Errorf("%w: no usable authentication method found", protocol.ErrAbort)
	}

	return config, nil
}

func (c *Connection) connectViaBastion(dst string, config *ssh.ClientConfig) error {
	bastion, err := c.Bastion.Connection()
	if err != nil {
		return fmt.Errorf("create bastion connection: %w", err)
	}
	bastionSSH, ok := bastion.(*Connection)
	if !ok {
		return fmt.Errorf("%w: bastion connection is not an SSH connection", protocol.ErrAbort)
	}
	c.Log().Debug("connecting to bastion", log.HostAttr(c), "bastion", bastionSSH)
	if err := bastionSSH.Connect(); err != nil {
		if errors.Is(err, hostkey.ErrHostKeyMismatch) {
			return fmt.Errorf("%w: bastion connect: %w", protocol.ErrAbort, err)
		}
		return err
	}
	bconn, err := bastionSSH.Dial("tcp", dst)
	if err != nil {
		return fmt.Errorf("bastion dial: %w", err)
	}
	client, chans, reqs, err := ssh.NewClientConn(bconn, dst, config)
	if err != nil {
		if errors.Is(err, hostkey.ErrHostKeyMismatch) {
			return fmt.Errorf("%w: bastion client connect: %w", protocol.ErrAbort, err)
		}
		return fmt.Errorf("bastion client connect: %w", err)
	}
	c.client = ssh.NewClient(client, chans, reqs)

	c.startKeepalive()

	return nil
}

func (c *Connection) startKeepalive() {
	if c.options.KeepAliveInterval == nil {
		return
	}

	c.done = make(chan struct{})
	go func() {
		ticker := time.NewTicker(*c.options.KeepAliveInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if !c.IsConnected() {
					close(c.done)
					return
				}
			case <-c.done:
				return
			}
		}
	}()
}

// Connect opens the SSH connection.
func (c *Connection) Connect() error {
	c.SetDefaults()

	config, err := c.clientConfig()
	if err != nil {
		return fmt.Errorf("%w: create config: %w", protocol.ErrAbort, err)
	}

	dst := net.JoinHostPort(c.Address, strconv.Itoa(c.Port))

	if c.Bastion != nil {
		return c.connectViaBastion(dst, config)
	}

	clientDirect, err := ssh.Dial("tcp", dst, config)
	if err != nil {
		if errors.Is(err, hostkey.ErrHostKeyMismatch) {
			return fmt.Errorf("%w: %w", protocol.ErrAbort, err)
		}
		return fmt.Errorf("ssh dial: %w", err)
	}
	c.client = clientDirect

	c.startKeepalive()

	return nil
}

func (c *Connection) pubkeySigner(signers []ssh.Signer, key ssh.PublicKey) (ssh.AuthMethod, error) {
	if len(signers) == 0 {
		return nil, fmt.Errorf("%w: signer not found for public key", protocol.ErrAbort)
	}

	for _, s := range signers {
		if bytes.Equal(key.Marshal(), s.PublicKey().Marshal()) {
			c.Log().Debug("signer for public key available in ssh agent")
			return ssh.PublicKeys(s), nil
		}
	}

	return nil, fmt.Errorf("%w: the provided key is a public key and is not known by agent", protocol.ErrAbort)
}

func (c *Connection) pkeySigner(signers []ssh.Signer, path string) (ssh.AuthMethod, error) {
	path, err := homedir.ExpandFile(path)
	if err != nil {
		return nil, fmt.Errorf("expand keyfile path: %w", err)
	}
	log.Trace(context.Background(), "checking identity file", log.KeyFile, path)
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: read identity file %s: %w", protocol.ErrAbort, path, err)
	}

	pubKey, _, _, _, err := ssh.ParseAuthorizedKey(key)
	if err == nil {
		log.Trace(context.Background(), "file is a public key", log.KeyFile, path)
		return c.pubkeySigner(signers, pubKey)
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err == nil {
		c.Log().Debug("using an unencrypted private key", log.KeyFile, path)
		return ssh.PublicKeys(signer), nil
	}

	var ppErr *ssh.PassphraseMissingError
	if errors.As(err, &ppErr) { //nolint:nestif
		c.Log().Debug("key is encrypted", log.KeyFile, path)

		if len(signers) > 0 {
			if signer, err := c.pkeySigner(signers, path+".pub"); err == nil {
				return signer, nil
			}
		}

		if c.PasswordCallback != nil {
			log.Trace(context.Background(), "asking for a password to decrypt key", log.HostAttr(c), log.KeyFile, path)
			pass, err := c.PasswordCallback()
			if err != nil {
				return nil, fmt.Errorf("%w: failed to get password: %w", protocol.ErrAbort, err)
			}
			signer, err := ssh.ParsePrivateKeyWithPassphrase(key, []byte(pass))
			if err != nil {
				return nil, fmt.Errorf("%w: encrypted key %s decoding failed: %w", protocol.ErrAbort, path, err)
			}
			return ssh.PublicKeys(signer), nil
		}
	}

	return nil, fmt.Errorf("%w: can't parse keyfile: %s: %w", protocol.ErrAbort, path, err)
}

// StartProcess executes a command on the remote host and uses the passed in streams for stdin, stdout and stderr. It returns a Waiter with a .Wait() function that
// blocks until the command finishes and returns an error if the exit code is not zero.
func (c *Connection) StartProcess(ctx context.Context, cmd string, stdin io.Reader, stdout, stderr io.Writer) (protocol.Waiter, error) {
	if c.client == nil {
		return nil, errNotConnected
	}

	session, err := c.client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("create ssh session: %w", err)
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
	session, err := c.client.NewSession()
	if err != nil {
		return fmt.Errorf("ssh new session: %w", err)
	}
	defer session.Close()

	session.Stdout = stdout
	session.Stderr = stderr
	var input io.Reader

	if inF, ok := stdin.(*os.File); ok {
		fd := int(os.Stdin.Fd())
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
	pass, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("failed to read password: %w", err)
	}
	return string(pass), nil
}
