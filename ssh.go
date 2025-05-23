package rig

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/acarl005/stripansi"
	"github.com/creasty/defaults"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/log"
	"github.com/k0sproject/rig/pkg/ssh/agent"
	"github.com/k0sproject/rig/pkg/ssh/hostkey"
	"github.com/kevinburke/ssh_config"
	"github.com/mattn/go-shellwords"
	ssh "golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

// SSH describes an SSH connection
type SSH struct {
	// Address of the remote host (IP or hostname)
	Address string `yaml:"address" json:"address" validate:"required,hostname_rfc1123|ip" jsonschema:"required,format=hostname,description=Address of the remote host (IP or hostname)"`

	// User to log in as
	User string `yaml:"user" json:"user" validate:"required" default:"root" jsonschema:"required,default=root,description=User to log in as"`

	// SSH port, usually 22
	Port int `yaml:"port" json:"port" validate:"gt=0,lte=65535" default:"22" jsonschema:"minimum=1,maximum=65535,default=22,description=SSH port, usually 22"`

	// Optional path to private key
	KeyPath *string `yaml:"keyPath,omitempty" json:"keyPath,omitempty" validate:"omitempty" jsonschema:"description=Optional path to private key"`

	// Optional known host key fingerprint
	HostKey string `yaml:"hostKey,omitempty" json:"hostKey,omitempty" jsonschema:"description=Optional known host key fingerprint"`

	// Optional bastion host
	Bastion *SSH `yaml:"bastion,omitempty" json:"bastion,omitempty" jsonschema:"description=Optional bastion host"`
	// Optional password callback function
	PasswordCallback PasswordCallback `yaml:"-" json:"-"`

	// AuthMethods can be used to pass in a list of ssh.AuthMethod objects
	// for example to use a private key from memory:
	//   ssh.PublicKeys(privateKey)
	// For convenience, you can use ParseSSHPrivateKey() to parse a private key:
	//   authMethods, err := rig.ParseSSHPrivateKey(key, rig.DefaultPassphraseCallback)
	AuthMethods []ssh.AuthMethod `yaml:"-" json:"-"`

	alias string
	name  string

	isWindows bool
	knowOs    bool
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

func (c *SSH) keypathsFromConfig() []string {
	log.Tracef("%s: trying to get a keyfile path from ssh config", c)
	idf := c.getConfigAll("IdentityFile")
	// https://github.com/kevinburke/ssh_config/blob/master/config.go#L254 says:
	// TODO: IdentityFile has multiple default values that we should return
	// To work around this, the hard coded list of known defaults are appended to the list
	idf = append(idf, defaultKeypaths...)
	sort.Strings(idf)
	idf = compact(idf)

	if len(idf) > 0 {
		log.Tracef("%s: detected %d identity file paths from ssh config: %v", c, len(idf), idf)
		return idf
	}
	log.Tracef("%s: no identity file paths found in ssh config", c)
	return []string{}
}

func (c *SSH) initGlobalDefaults() {
	log.Tracef("discovering global default keypaths")
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
	log.Tracef("global default keypaths from ssh config: %+v", dummyhostKeyPaths)
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
				log.Tracef("%s: %s: %v", c, p, err)
				continue
			}
			log.Debugf("%s: using identity file %s", c, expanded)
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
		c.name = fmt.Sprintf("[ssh] %s", net.JoinHostPort(c.Address, strconv.Itoa(c.Port)))
	}

	return c.name
}

// IsConnected returns true if the client is connected
func (c *SSH) IsConnected() bool {
	return c.client != nil
}

// Disconnect closes the SSH connection
func (c *SSH) Disconnect() {
	c.client.Close()
}

// IsWindows is true when the host is running windows
func (c *SSH) IsWindows() bool {
	if !c.knowOs && c.client != nil {
		log.Debugf("%s: checking if host is windows", c)
		if strings.Contains(string(c.client.ServerVersion()), "Windows") {
			c.isWindows = true
			c.knowOs = true
			return true
		}
		c.isWindows = c.Exec("cmd.exe /c exit 0") == nil
		log.Debugf("%s: host is windows: %t", c, c.isWindows)
		c.knowOs = true
	}

	return c.isWindows
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
		log.Debugf("%s: StrictHostkeyChecking is set to 'no'", c)
		return true
	}

	return false
}

func shouldHash(c *SSH) bool {
	var hash bool
	if hashKnownHosts := c.getConfigAll("HashKnownHosts"); len(hashKnownHosts) == 1 {
		hash := hashKnownHosts[0] == "yes"
		if hash {
			log.Debugf("%s: HashKnownHosts is set to %q, will hash newly added keys", c, hashKnownHosts[0])
		} else {
			log.Debugf("%s: HashKnownHosts is set to %q, won't hash newly added keys", c, hashKnownHosts[0])
		}
	}
	return hash
}

func (c *SSH) hostkeyCallback() (ssh.HostKeyCallback, error) {
	if c.HostKey != "" {
		log.Debugf("%s: using host key from config", c)
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
		log.Tracef("%s: using known_hosts file from SSH_KNOWN_HOSTS: %s", c, path)
		return knownhostsCallback(path, permissive, hash)
	}

	var khPath string

	// Ask ssh_config for a known hosts file
	kfs := c.getConfigAll("UserKnownHostsFile")
	// splitting the result as for some reason ssh_config sometimes seems to
	// return a single string containing space separated paths
	if files, err := shellwords.Parse(strings.Join(kfs, " ")); err == nil {
		for _, f := range files {
			log.Tracef("%s: trying known_hosts file from ssh config %s", c, f)
			exp, err := expandPath(f)
			if err == nil {
				khPath = exp
				break
			}
		}
	}

	if khPath != "" {
		log.Tracef("%s: using known_hosts file from ssh config %s", c, khPath)
		return knownhostsCallback(khPath, permissive, hash)
	}

	log.Tracef("%s: using default known_hosts file %s", c, hostkey.DefaultKnownHostsPath)
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
		log.Tracef("%s: failed to get ssh agent client: %v", c, err)
	} else {
		signers, err = agent.Signers()
		if err != nil {
			log.Debugf("%s: failed to list signers from ssh agent: %v", c, err)
		}
	}

	if len(c.AuthMethods) > 0 {
		log.Tracef("%s: using %d passed-in auth methods", c, len(c.AuthMethods))
		config.Auth = c.AuthMethods
	} else if len(signers) > 0 {
		log.Debugf("%s: using all keys (%d) from ssh agent because a keypath was not explicitly given", c, len(signers))
		config.Auth = append(config.Auth, ssh.PublicKeys(signers...))
	}

	for _, keyPath := range c.keyPaths {
		if am, ok := authMethodCache.Load(keyPath); ok {
			switch authM := am.(type) {
			case ssh.AuthMethod:
				log.Tracef("%s: using cached auth method for %s", c, keyPath)
				config.Auth = append(config.Auth, authM)
			case error:
				log.Tracef("%s: already discarded key %s: %v", c, keyPath, authM)
			default:
				log.Tracef("%s: unexpected type %T for cached auth method for %s", c, am, keyPath)
			}
			continue
		}
		privateKeyAuth, err := c.pkeySigner(signers, keyPath)
		if err != nil {
			log.Debugf("%s: failed to obtain a signer for identity %s: %v", c, keyPath, err)
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
			log.Debugf("%s: signer for public key available in ssh agent", c)
			return ssh.PublicKeys(s), nil
		}
	}

	return nil, fmt.Errorf("%w: the provided key is a public key and is not known by agent", ErrAuthFailed)
}

func (c *SSH) pkeySigner(signers []ssh.Signer, path string) (ssh.AuthMethod, error) {
	log.Tracef("%s: checking identity file %s", c, path)
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: read identity file %s: %w", ErrCantConnect, path, err)
	}

	pubKey, _, _, _, err := ssh.ParseAuthorizedKey(key)
	if err == nil {
		log.Debugf("%s: file %s is a public key", c, path)
		return c.pubkeySigner(signers, pubKey)
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err == nil {
		log.Debugf("%s: using an unencrypted private key from %s", c, path)
		return ssh.PublicKeys(signer), nil
	}

	var ppErr *ssh.PassphraseMissingError
	if errors.As(err, &ppErr) { //nolint:nestif
		log.Debugf("%s: key %s is encrypted", c, path)

		if len(signers) > 0 {
			if signer, err := c.pkeySigner(signers, path+".pub"); err == nil {
				return signer, nil
			}
		}

		if c.PasswordCallback != nil {
			log.Tracef("%s: asking for a password to decrypt %s", c, path)
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

const (
	ptyWidth  = 80
	ptyHeight = 40
)

// ExecStreams executes a command on the remote host and uses the passed in streams for stdin, stdout and stderr. It returns a Waiter with a .Wait() function that
// blocks until the command finishes and returns an error if the exit code is not zero.
func (c *SSH) ExecStreams(cmd string, stdin io.ReadCloser, stdout, stderr io.Writer, opts ...exec.Option) (exec.Waiter, error) {
	if c.client == nil {
		return nil, ErrNotConnected
	}

	execOpts := exec.Build(opts...)
	cmd, err := execOpts.Command(cmd)
	if err != nil {
		return nil, fmt.Errorf("%w: build command: %w", ErrCommandFailed, err)
	}

	execOpts.LogCmd(c.String(), cmd)

	session, err := c.client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("%w: create new session: %w", ErrCommandFailed, err)
	}

	session.Stdin = stdin
	session.Stdout = stdout
	session.Stderr = stderr

	if err := session.Start(cmd); err != nil {
		return nil, fmt.Errorf("%w: start session: %w", ErrCommandFailed, err)
	}

	return session, nil
}

// Exec executes a command on the host
func (c *SSH) Exec(cmd string, opts ...exec.Option) error { //nolint:gocognit,cyclop
	execOpts := exec.Build(opts...)
	session, err := c.client.NewSession()
	if err != nil {
		return fmt.Errorf("%w: ssh new session: %w", ErrCommandFailed, err)
	}
	defer session.Close()

	cmd, err = execOpts.Command(cmd)
	if err != nil {
		return fmt.Errorf("%w: build command: %w", ErrCommandFailed, err)
	}

	if len(execOpts.Stdin) == 0 && c.knowOs && !c.isWindows {
		// Only request a PTY when there's no STDIN data, because
		// then you would need to send a CTRL-D after input to signal
		// the end of text
		modes := ssh.TerminalModes{ssh.ECHO: 0}
		err = session.RequestPty("xterm", ptyWidth, ptyHeight, modes)
		if err != nil {
			return fmt.Errorf("request pty: %w", err)
		}
	}

	execOpts.LogCmd(c.String(), cmd)

	stdin, _ := session.StdinPipe()
	stdout, _ := session.StdoutPipe()
	stderr, _ := session.StderrPipe()

	if err := session.Start(cmd); err != nil {
		return fmt.Errorf("%w: ssh session start: %w", ErrCommandFailed, err)
	}

	if len(execOpts.Stdin) > 0 {
		execOpts.LogStdin(c.String())
		if _, err := io.WriteString(stdin, execOpts.Stdin); err != nil {
			return fmt.Errorf("%w: write stdin: %w", ErrCommandFailed, err)
		}
	}
	stdin.Close()

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		if execOpts.Writer == nil {
			outputScanner := bufio.NewScanner(stdout)

			for outputScanner.Scan() {
				text := outputScanner.Text()
				stripped := stripansi.Strip(text)
				execOpts.AddOutput(c.String(), stripped+"\n", "")
			}

			if err := outputScanner.Err(); err != nil {
				execOpts.LogErrorf("%s: %s", c, err.Error())
			}
		} else {
			if _, err := io.Copy(execOpts.Writer, stdout); err != nil {
				execOpts.LogErrorf("%s: failed to stream stdout: %v", c, err)
			}
		}
	}()

	var errors []string

	wg.Add(1)
	go func() {
		defer wg.Done()
		outputScanner := bufio.NewScanner(stderr)

		for outputScanner.Scan() {
			msg := outputScanner.Text()
			if msg != "" {
				errors = append(errors, msg)
				execOpts.LogErrorf("%s: %s", c, msg)
			}
		}

		if err := outputScanner.Err(); err != nil {
			execOpts.LogErrorf("%s: %s", c, err.Error())
		}
	}()

	err = session.Wait()
	wg.Wait()

	if err != nil {
		return fmt.Errorf("ssh session wait: %w", err)
	}

	if c.knowOs && c.isWindows && (!execOpts.AllowWinStderr && len(errors) > 0) {
		return fmt.Errorf("%w: received data in stderr: %s", ErrCommandFailed, strings.Join(errors, "\n"))
	}

	return nil
}

// ExecInteractive executes a command on the host and copies stdin/stdout/stderr from local host
func (c *SSH) ExecInteractive(cmd string) error {
	session, err := c.client.NewSession()
	if err != nil {
		return fmt.Errorf("%w: ssh new session: %w", ErrCommandFailed, err)
	}
	defer session.Close()

	session.Stdout = os.Stdout
	session.Stderr = os.Stderr

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

	stdinpipe, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("%w: get stdin pipe: %w", ErrCommandFailed, err)
	}
	go func() {
		_, _ = io.Copy(stdinpipe, os.Stdin)
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
