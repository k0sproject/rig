package rig

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/creasty/defaults"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/log"
	"github.com/k0sproject/rig/ssh/agent"
	"github.com/k0sproject/rig/ssh/hostkey"
	"github.com/kevinburke/ssh_config"
	"github.com/mattn/go-shellwords"
	ssh "golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

// SSH describes an SSH connection
type SSH struct {
	log.LoggerInjectable `yaml:"-"`

	Address          string           `yaml:"address" validate:"required,hostname_rfc1123|ip"`
	User             string           `yaml:"user" validate:"required" default:"root"`
	Port             int              `yaml:"port" default:"22" validate:"gt=0,lte=65535"`
	KeyPath          *string          `yaml:"keyPath" validate:"omitempty"`
	HostKey          string           `yaml:"hostKey,omitempty"`
	Bastion          *SSH             `yaml:"bastion,omitempty"`
	PasswordCallback PasswordCallback `yaml:"-"`

	// AuthMethods can be used to pass in a list of ssh.AuthMethod objects
	// for example to use a private key from memory:
	//   ssh.PublicKeys(privateKey)
	// For convenience, you can use ParseSSHPrivateKey() to parse a private key:
	//   authMethods, err := rig.ParseSSHPrivateKey(key, rig.DefaultPassphraseCallback)
	AuthMethods []ssh.AuthMethod `yaml:"-"`

	alias string
	name  string

	isWindows *bool
	once      sync.Once

	client *ssh.Client

	keyPaths []string
}

// PasswordCallback is a function that is called when a passphrase is needed to decrypt a private key
type PasswordCallback func() (secret string, err error)

var (
	authMethodCache   = sync.Map{}
	defaultKeypaths   = []string{"~/.ssh/id_rsa", "~/.ssh/identity", "~/.ssh/id_dsa", "~/.ssh/id_ecdsa", "~/.ssh/id_ed25519"}
	dummyhostKeyPaths []string
	globalOnce        sync.Once
	knownHostsMU      sync.Mutex

	// ErrChecksumMismatch is returned when the checksum of an uploaded file does not match expectation
	ErrChecksumMismatch = errors.New("checksum mismatch")
)

const hopefullyNonexistentHost = "thisH0stDoe5not3xist"

// returns the current user homedir, prefers $HOME env var
func homeDir() (string, error) {
	if home, ok := os.LookupEnv("HOME"); ok {
		return home, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}
	return home, nil
}

// does ~/ style dir expansion for files under current user home. ~user/ style paths are not supported.
func expandPath(path string) (string, error) {
	if path[0] != '~' {
		return path, nil
	}
	if len(path) == 1 {
		return homeDir()
	}
	if path[1] != '/' {
		return "", fmt.Errorf("%w: ~user/ style paths not supported", ErrNotImplemented)
	}

	home, err := homeDir()
	if err != nil {
		return "", err
	}
	return home + path[1:], nil
}

func expandAndValidatePath(path string) (string, error) {
	if len(path) == 0 {
		return "", fmt.Errorf("%w: path is empty", ErrInvalidPath)
	}

	path, err := expandPath(path)
	if err != nil {
		return "", err
	}
	stat, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidPath, err)
	}

	if stat.IsDir() {
		return "", fmt.Errorf("%w: %s is a directory", ErrInvalidPath, path)
	}

	return path, nil
}

// compact replaces consecutive runs of equal elements with a single copy.
// This is like the uniq command found on Unix.
//
// Taken from stdlib's slices package, to work around a problem on github actions
// (package slices is not in GOROOT (/opt/hostedtoolcache/go/1.20.12/x64/src/slices)
func compact[S ~[]E, E comparable](slice S) S {
	if len(slice) < 2 {
		return slice
	}
	i := 1
	for k := 1; k < len(slice); k++ {
		if slice[k] != slice[k-1] {
			if i != k {
				slice[i] = slice[k]
			}
			i++
		}
	}
	return slice[:i]
}

// Client implements the ClientConfigurer interface
func (c *SSH) Client() (Client, error) {
	return c, nil
}

func (c *SSH) keypathsFromConfig() []string {
	c.Log().Tracef("trying to get a keyfile path from ssh config")
	idf := c.getConfigAll("IdentityFile")
	// https://github.com/kevinburke/ssh_config/blob/master/config.go#L254 says:
	// TODO: IdentityFile has multiple default values that we should return
	// To work around this, the hard coded list of known defaults are appended to the list
	idf = append(idf, defaultKeypaths...)
	sort.Strings(idf)
	idf = compact(idf)

	if len(idf) > 0 {
		c.Log().Tracef("detected %d identity file paths from ssh config: %v", len(idf), idf)
		return idf
	}
	c.Log().Tracef("no identity file paths found in ssh config")
	return []string{}
}

func (c *SSH) initGlobalDefaults() {
	c.Log().Tracef("discovering global default keypaths")
	dummyHostIdentityFiles := SSHConfigGetAll(hopefullyNonexistentHost, "IdentityFile")
	// https://github.com/kevinburke/ssh_config/blob/master/config.go#L254 says:
	// TODO: IdentityFile has multiple default values that we should return
	// To work around this, the hard coded list of known defaults are appended to the list
	dummyHostIdentityFiles = append(dummyHostIdentityFiles, defaultKeypaths...)
	sort.Strings(dummyHostIdentityFiles)
	dummyHostIdentityFiles = compact(dummyHostIdentityFiles)
	for _, keyPath := range dummyHostIdentityFiles {
		if expanded, err := expandAndValidatePath(keyPath); err == nil {
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

// SetDefaults sets various default values
func (c *SSH) SetDefaults() {
	globalOnce.Do(c.initGlobalDefaults)
	c.once.Do(func() {
		if c.KeyPath != nil && *c.KeyPath != "" {
			if expanded, err := expandAndValidatePath(*c.KeyPath); err == nil {
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
			expanded, err := expandAndValidatePath(p)
			if err != nil {
				c.Log().Tracef("expand and validate %s: %v", p, err)
				continue
			}
			c.Log().Debugf("using identity file %s", expanded)
			c.keyPaths = append(c.keyPaths, expanded)
		}

		// check if all the paths that were found are global defaults
		// errors are handled differently when a keypath is explicitly set vs when it's defaulted
		if uniq, found := findUniq(c.keyPaths, dummyhostKeyPaths); found {
			c.KeyPath = &uniq
		}
	})
}

// Protocol returns the protocol name, "SSH"
func (c *SSH) Protocol() string {
	return "SSH"
}

// IPAddress returns the connection address
func (c *SSH) IPAddress() string {
	return c.Address
}

// SSHConfigGetAll by default points to ssh_config package's GetAll() function
// you can override it with your own implementation for testing purposes
var SSHConfigGetAll = ssh_config.GetAll

func (c *SSH) getConfigAll(key string) []string {
	if c.alias != "" {
		return SSHConfigGetAll(c.alias, key)
	}
	return SSHConfigGetAll(c.Address, key)
}

// String returns the connection's printable name
func (c *SSH) String() string {
	if c.name == "" {
		c.name = "[ssh] " + net.JoinHostPort(c.Address, strconv.Itoa(c.Port))
	}

	return c.name
}

// Disconnect closes the SSH connection
func (c *SSH) Disconnect() {
	if c.client == nil {
		return
	}
	c.client.Close()
}

// IsWindows is true when the host is running windows
func (c *SSH) IsWindows() bool {
	if c.isWindows != nil {
		return *c.isWindows
	}

	if c.client == nil {
		return false
	}

	var isWin bool
	if strings.Contains(string(c.client.ServerVersion()), "Windows") {
		isWin = true
		c.isWindows = &isWin
		return true
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	isWinProc, err := c.StartProcess(ctx, "cmd.exe /c exit 0", nil, nil, nil)
	isWin = err == nil && isWinProc.Wait() == nil

	c.isWindows = &isWin
	c.Log().Debugf("host is windows: %t", *c.isWindows)

	return *c.isWindows
}

func knownhostsCallback(path string, permissive, hash bool) (ssh.HostKeyCallback, error) {
	cb, err := hostkey.KnownHostsFileCallback(path, permissive, hash)
	if err != nil {
		return nil, fmt.Errorf("%w: create host key validator: %w", ErrCantConnect, err)
	}
	return cb, nil
}

func isPermissive(c *SSH) bool {
	if strict := c.getConfigAll("StrictHostkeyChecking"); len(strict) > 0 && strict[0] == "no" {
		c.Log().Debugf("StrictHostkeyChecking is set to 'no'")
		return true
	}

	return false
}

func shouldHash(c *SSH) bool {
	var hash bool
	if hashKnownHosts := c.getConfigAll("HashKnownHosts"); len(hashKnownHosts) == 1 {
		hash := hashKnownHosts[0] == "yes"
		if hash {
			c.Log().Debugf("HashKnownHosts is set to %q, known hosts file keys will be hashed", hashKnownHosts[0])
		}
	}
	return hash
}

func (c *SSH) hostkeyCallback() (ssh.HostKeyCallback, error) {
	if c.HostKey != "" {
		c.Log().Debugf("using host key from config")
		return hostkey.StaticKeyCallback(c.HostKey), nil
	}

	knownHostsMU.Lock()
	defer knownHostsMU.Unlock()

	permissive := isPermissive(c)
	hash := shouldHash(c)

	if path, ok := hostkey.KnownHostsPathFromEnv(); ok {
		if path == "" {
			return hostkey.InsecureIgnoreHostKeyCallback, nil
		}
		c.Log().Debugf("%s: using known_hosts file from SSH_KNOWN_HOSTS: %s", path)
		return knownhostsCallback(path, permissive, hash)
	}

	var khPath string

	// Ask ssh_config for a known hosts file
	kfs := c.getConfigAll("UserKnownHostsFile")
	// splitting the result as for some reason ssh_config sometimes seems to
	// return a single string containing space separated paths
	if files, err := shellwords.Parse(strings.Join(kfs, " ")); err == nil {
		for _, f := range files {
			c.Log().Tracef("trying known_hosts file from ssh config %s", f)
			exp, err := expandPath(f)
			if err == nil {
				khPath = exp
				break
			}
		}
	}

	if khPath != "" {
		c.Log().Tracef("using known_hosts file from ssh config %s", khPath)
		return knownhostsCallback(khPath, permissive, hash)
	}

	c.Log().Tracef("using default known_hosts file %s", hostkey.DefaultKnownHostsPath)
	defaultPath, err := expandPath(hostkey.DefaultKnownHostsPath)
	if err != nil {
		return nil, err
	}

	return knownhostsCallback(defaultPath, permissive, hash)
}

func (c *SSH) clientConfig() (*ssh.ClientConfig, error) { //nolint:cyclop
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
		c.Log().Tracef("failed to get ssh agent client: %v", err)
	} else {
		c.Log().Debugf("using ssh agent")
		signers, err = agent.Signers()
		if err != nil {
			c.Log().Debugf("failed to list signers from ssh agent: %v", c, err)
		}
	}

	if len(c.AuthMethods) > 0 {
		c.Log().Tracef("using %d passed-in auth methods", len(c.AuthMethods))
		config.Auth = c.AuthMethods
	} else if len(signers) > 0 {
		c.Log().Debugf("%s: using all keys (%d) from ssh agent because a keypath was not explicitly given", c, len(signers))
		config.Auth = append(config.Auth, ssh.PublicKeys(signers...))
	}

	for _, keyPath := range c.keyPaths {
		if am, ok := authMethodCache.Load(keyPath); ok {
			switch authM := am.(type) {
			case ssh.AuthMethod:
				c.Log().Tracef("using cached auth method for %s", keyPath)
				config.Auth = append(config.Auth, authM)
			case error:
				c.Log().Tracef("already discarded key %s: %v", keyPath, authM)
			default:
				c.Log().Tracef("unexpected type %T for cached auth method for %s", am, keyPath)
			}
			continue
		}
		privateKeyAuth, err := c.pkeySigner(signers, keyPath)
		if err != nil {
			c.Log().Debugf("failed to obtain a signer for identity %s: %v", keyPath, err)
			// store the error so this key won't be loaded again
			authMethodCache.Store(keyPath, err)
		} else {
			authMethodCache.Store(keyPath, privateKeyAuth)
			config.Auth = append(config.Auth, privateKeyAuth)
		}
	}

	if len(config.Auth) == 0 {
		return nil, fmt.Errorf("%w: no usable authentication method found", ErrCantConnect)
	}

	return config, nil
}

// Connect opens the SSH connection
func (c *SSH) Connect() error {
	if err := defaults.Set(c); err != nil {
		return fmt.Errorf("%w: set defaults: %w", ErrValidationFailed, err)
	}

	config, err := c.clientConfig()
	if err != nil {
		return fmt.Errorf("%w: create config: %w", ErrCantConnect, err)
	}

	dst := net.JoinHostPort(c.Address, strconv.Itoa(c.Port))

	if c.Bastion == nil {
		clientDirect, err := ssh.Dial("tcp", dst, config)
		if err != nil {
			if errors.Is(err, hostkey.ErrHostKeyMismatch) {
				return fmt.Errorf("%w: %w", ErrCantConnect, err)
			}
			return fmt.Errorf("ssh dial: %w", err)
		}
		c.client = clientDirect
		return nil
	}

	if err := c.Bastion.Connect(); err != nil {
		if errors.Is(err, hostkey.ErrHostKeyMismatch) {
			return fmt.Errorf("%w: bastion connect: %w", ErrCantConnect, err)
		}
		return err
	}
	bconn, err := c.Bastion.client.Dial("tcp", dst)
	if err != nil {
		return fmt.Errorf("bastion dial: %w", err)
	}
	client, chans, reqs, err := ssh.NewClientConn(bconn, dst, config)
	if err != nil {
		if errors.Is(err, hostkey.ErrHostKeyMismatch) {
			return fmt.Errorf("%w: bastion client connect: %w", ErrCantConnect, err)
		}
		return fmt.Errorf("bastion client connect: %w", err)
	}
	c.client = ssh.NewClient(client, chans, reqs)

	return nil
}

func (c *SSH) pubkeySigner(signers []ssh.Signer, key ssh.PublicKey) (ssh.AuthMethod, error) {
	if len(signers) == 0 {
		return nil, fmt.Errorf("%w: signer not found for public key", ErrCantConnect)
	}

	for _, s := range signers {
		if bytes.Equal(key.Marshal(), s.PublicKey().Marshal()) {
			c.Log().Debugf("signer for public key available in ssh agent")
			return ssh.PublicKeys(s), nil
		}
	}

	return nil, fmt.Errorf("%w: the provided key is a public key and is not known by agent", ErrAuthFailed)
}

func (c *SSH) pkeySigner(signers []ssh.Signer, path string) (ssh.AuthMethod, error) {
	c.Log().Tracef("checking identity file %s", path)
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: read identity file %s: %w", ErrCantConnect, path, err)
	}

	pubKey, _, _, _, err := ssh.ParseAuthorizedKey(key)
	if err == nil {
		c.Log().Debugf("file %s is a public key", path)
		return c.pubkeySigner(signers, pubKey)
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err == nil {
		c.Log().Debugf("using an unencrypted private key from %s", path)
		return ssh.PublicKeys(signer), nil
	}

	var ppErr *ssh.PassphraseMissingError
	if errors.As(err, &ppErr) { //nolint:nestif
		c.Log().Debugf("key %s is encrypted", path)

		if len(signers) > 0 {
			if signer, err := c.pkeySigner(signers, path+".pub"); err == nil {
				return signer, nil
			}
		}

		if c.PasswordCallback != nil {
			c.Log().Tracef("%s: asking for a password to decrypt %s", c, path)
			pass, err := c.PasswordCallback()
			if err != nil {
				return nil, fmt.Errorf("%w: password provider failed: %w", ErrCantConnect, err)
			}
			signer, err := ssh.ParsePrivateKeyWithPassphrase(key, []byte(pass))
			if err != nil {
				return nil, fmt.Errorf("%w: protected key %s decoding failed: %w", ErrCantConnect, path, err)
			}
			return ssh.PublicKeys(signer), nil
		}
	}

	return nil, fmt.Errorf("%w: can't parse keyfile: %s: %w", ErrCantConnect, path, err)
}

// StartProcess executes a command on the remote host and uses the passed in streams for stdin, stdout and stderr. It returns a Waiter with a .Wait() function that
// blocks until the command finishes and returns an error if the exit code is not zero.
func (c *SSH) StartProcess(ctx context.Context, cmd string, stdin io.Reader, stdout, stderr io.Writer) (exec.Waiter, error) {
	if c.client == nil {
		return nil, ErrNotConnected
	}

	session, err := c.client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("%w: create new session: %w", ErrCommandFailed, err)
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

// ExecInteractive executes a command on the host and copies stdin/stdout/stderr from local host
func (c *SSH) ExecInteractive(cmd string, stdin io.Reader, stdout, stderr io.Writer) error {
	session, err := c.client.NewSession()
	if err != nil {
		return fmt.Errorf("%w: ssh new session: %w", ErrCommandFailed, err)
	}
	defer session.Close()

	session.Stdout = stdout
	session.Stderr = stderr
	var input io.Reader

	if inF, ok := stdin.(*os.File); ok {
		fd := int(os.Stdin.Fd())
		old, err := term.MakeRaw(fd)
		if err != nil {
			return fmt.Errorf("%w: make local terminal raw: %w", ErrOS, err)
		}

		defer func(fd int, old *term.State) {
			_ = term.Restore(fd, old)
		}(fd, old)

		rows, cols, err := term.GetSize(fd)
		if err != nil {
			return fmt.Errorf("%w: get terminal size: %w", ErrOS, err)
		}

		modes := ssh.TerminalModes{ssh.ECHO: 1}
		err = session.RequestPty("xterm", cols, rows, modes)
		if err != nil {
			return fmt.Errorf("%w: request pty: %w", ErrCommandFailed, err)
		}
		input = inF
	} else {
		input = stdin
	}

	stdinpipe, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("%w: get stdin pipe: %w", ErrCommandFailed, err)
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
		return fmt.Errorf("%w: ssh session: %w", ErrCommandFailed, err)
	}

	if err := session.Wait(); err != nil {
		return fmt.Errorf("%w: ssh session wait: %w", ErrCommandFailed, err)
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

// DefaultPasswordCallback is a default implementation for PasswordCallback
func DefaultPasswordCallback() (string, error) {
	fmt.Print("Enter passphrase: ")
	pass, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("failed to read password: %w", err)
	}
	return string(pass), nil
}
