package rig

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/acarl005/stripansi"
	"github.com/creasty/defaults"
	"github.com/google/shlex"
	"github.com/k0sproject/rig/errstring"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/log"
	"github.com/k0sproject/rig/pkg/ssh/hostkey"
	"github.com/kevinburke/ssh_config"
	ssh "golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

// SSH describes an SSH connection
type SSH struct {
	Address          string           `yaml:"address" validate:"required,hostname|ip"`
	User             string           `yaml:"user" validate:"required" default:"root"`
	Port             *int             `yaml:"port" default:"22" validate:"gt=0,lte=65535"`
	KeyPath          *string          `yaml:"keyPath" validate:"omitempty"`
	HostKey          string           `yaml:"hostKey,omitempty"`
	Bastion          *SSH             `yaml:"bastion,omitempty"`
	PasswordCallback PasswordCallback `yaml:"-"`

	connAddr string

	isWindows bool
	knowOs    bool
	once      sync.Once

	client *ssh.Client

	keyPaths []string
}

// PasswordCallback is a function that is called when a passphrase is needed to decrypt a private key
type PasswordCallback func() (secret string, err error)

type sshconfig interface {
	GetAll(alias, key string) []string
	Get(alias, key string) string
}

var (
	authMethodCache = sync.Map{}
	defaultKeypaths = []string{}
	globalOnce      sync.Once
	knownHostsMU    sync.Mutex

	sshConfig sshconfig = ssh_config.DefaultUserSettings

	// ErrChecksumMismatch is returned when the checksum of an uploaded file does not match expectation
	ErrChecksumMismatch = errstring.New("checksum mismatch")
)

// returns the current user homedir, prefers $HOME env var
func homeDir() (string, error) {
	if home, ok := os.LookupEnv("HOME"); ok {
		return home, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", ErrOS.Wrapf("failed to get user home directory: %w", err)
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
		return "", ErrNotImplemented.Wrapf("~user/ style paths not supported")
	}

	home, err := homeDir()
	if err != nil {
		return "", err
	}
	return home + path[1:], nil
}

func expandAndValidatePath(path string) (string, error) {
	if len(path) == 0 {
		return "", ErrInvalidPath.Wrapf("path is empty")
	}

	path, err := expandPath(path)
	if err != nil {
		return "", err
	}
	stat, err := os.Stat(path)
	if err != nil {
		return "", ErrInvalidPath.Wrap(err)
	}

	if stat.IsDir() {
		return "", ErrInvalidPath.Wrapf("%s is a directory", path)
	}

	return path, nil
}

func flattenPaths(paths []string) []string {
	var out []string
	for _, p := range paths {
		pp, err := shlex.Split(p)
		if err == nil {
			out = append(out, pp...)
		}
	}
	return out
}

func uniqStrings(elems []string) []string {
	if len(elems) < 2 {
		return elems
	}
	uniq := make(map[string]struct{})
	for _, e := range elems {
		uniq[e] = struct{}{}
	}
	out := make([]string, 0, len(uniq))
	for k := range uniq {
		out = append(out, k)
	}
	return out
}

func initSSHDefaults() {
	keyPaths := sshConfig.GetAll("*", "IdentityFile")
	if len(keyPaths) > 0 {
		defaultKeypaths = flattenPaths(keyPaths)
	}
}

func (c *SSH) nonDefaultKeypaths() []string {
	var keyPaths []string
	for _, p := range c.keyPaths {
		var found bool
		for _, d := range defaultKeypaths {
			if p == d {
				found = true
				break
			}
		}
		if !found {
			keyPaths = append(keyPaths, p)
		}
	}
	return keyPaths
}

func intPtr(num int) *int {
	return &num
}

func (c *SSH) setupAddress() {
	if addr := sshConfig.Get(c.Address, "HostName"); addr != "" {
		log.Debugf("%s: using hostname %s from ssh config as connection address", c.Address, addr)
		c.connAddr = addr
		return
	}
	c.connAddr = c.Address
}

func (c *SSH) setupPort() {
	if c.Port != nil {
		return
	}

	portS := sshConfig.Get(c.Address, "Port")
	port, err := strconv.Atoi(portS)
	if err == nil {
		c.Port = intPtr(port)
		log.Tracef("%s: using port %d from ssh config", c, c.Port)
		return
	}
	c.Port = intPtr(22)
	log.Tracef("%s: using default port", c)
}

func (c *SSH) setupKeyPaths() {
	if sshConfig.Get(c.Address, "PubkeyAuthentication") == "no" {
		log.Infof("%s: public key based authentication disabled in ssh config", c)
		c.keyPaths = nil
		return
	}

	// if keypath is set, use that
	if c.KeyPath != nil && *c.KeyPath != "" {
		c.keyPaths = []string{*c.KeyPath}
		return
	}

	log.Tracef("%s: trying to get a keyfile path from ssh config", c)
	if paths := sshConfig.GetAll(c.Address, "IdentityFile"); len(paths) > 0 {
		c.keyPaths = uniqStrings(flattenPaths(paths))
		log.Tracef("%s: detected %d identity file paths from ssh config: %v", c, len(c.keyPaths), c.keyPaths)
		return
	}
}

func (c *SSH) sanitizeKeyPaths() {
	if len(c.keyPaths) == 0 {
		return
	}
	var newPaths []string
	for _, p := range c.keyPaths {
		if p == "" {
			continue
		}
		p, err := expandAndValidatePath(p)
		if err != nil {
			log.Tracef("%s: failed to validate key path %s: %v", c, p, err)
			continue
		}
		newPaths = append(newPaths, p)
	}
	c.keyPaths = uniqStrings(newPaths)
}

func (c *SSH) setup() {
	for _, f := range []func(){
		c.setupAddress,
		c.setupPort,
		c.setupKeyPaths,
		c.sanitizeKeyPaths,
	} {
		f()
	}
}

// SetDefaults sets various default values
func (c *SSH) SetDefaults() {
	globalOnce.Do(initSSHDefaults)
	c.once.Do(c.setup)
}

// Protocol returns the protocol name, "SSH"
func (c *SSH) Protocol() string {
	return "SSH"
}

// IPAddress returns the connection address
func (c *SSH) IPAddress() string {
	return c.Address
}

// try with port, if no results, try without
func (c *SSH) getConfigAll(key string) []string {
	return sshConfig.GetAll(c.Address, key)
}

// String returns the connection's printable name
func (c *SSH) String() string {
	var name string
	if c.connAddr != c.Address {
		name = c.Address
	} else {
		name = c.connAddr
	}
	return fmt.Sprintf("[ssh] %s", net.JoinHostPort(name, strconv.Itoa(*c.Port)))
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

func knownhostsCallback(path string, permissive bool) (ssh.HostKeyCallback, error) {
	cb, err := hostkey.KnownHostsFileCallback(path, permissive)
	if err != nil {
		return nil, ErrCantConnect.Wrapf("create host key validator: %w", err)
	}
	return cb, nil
}

func (c *SSH) hostkeyCallback() (ssh.HostKeyCallback, error) {
	if c.HostKey != "" {
		log.Debugf("%s: using host key from config", c)
		return hostkey.StaticKeyCallback(c.HostKey), nil
	}

	knownHostsMU.Lock()
	defer knownHostsMU.Unlock()

	var permissive bool
	strict := c.getConfigAll("StrictHostkeyChecking")
	if len(strict) > 0 && strict[0] == "no" {
		log.Debugf("%s: StrictHostkeyChecking is set to 'no'", c)
		permissive = true
	}

	if path, ok := hostkey.KnownHostsPathFromEnv(); ok {
		if path == "" {
			return hostkey.InsecureIgnoreHostKeyCallback, nil
		}
		log.Tracef("%s: using known_hosts file from SSH_KNOWN_HOSTS: %s", c, path)
		return knownhostsCallback(path, permissive)
	}

	var khPath string

	// Ask ssh_config for a known hosts file
	kfs := c.getConfigAll("UserKnownHostsFile")
	// splitting the result as for some reason ssh_config sometimes seems to
	// return a single string containing space separated paths
	if files, err := shlex.Split(strings.Join(kfs, " ")); err == nil {
		for _, f := range files {
			exp, err := expandPath(f)
			if err == nil {
				khPath = exp
				break
			}
		}
	}

	if khPath != "" {
		log.Tracef("%s: using known_hosts file from ssh config %s", c, khPath)
		return knownhostsCallback(khPath, permissive)
	}

	log.Tracef("%s: using default known_hosts file %s", c, hostkey.DefaultKnownHostsPath)
	defaultPath, err := expandPath(hostkey.DefaultKnownHostsPath)
	if err != nil {
		return nil, err
	}

	return knownhostsCallback(defaultPath, permissive)
}

func (c *SSH) signers() []ssh.Signer {
	if sshConfig.Get(c.Address, "IdentitiesOnly") == "yes" {
		log.Debugf("%s: IdentitiesOnly is set to 'yes', not using ssh-agent", c)
		return []ssh.Signer{}
	}
	agent, err := agentClient()
	if err != nil {
		log.Tracef("%s: failed to get ssh agent client: %v", c, err)
		return []ssh.Signer{}
	}
	signers, err := agent.Signers()
	if err != nil {
		log.Debugf("%s: failed to list signers from ssh agent: %v", c, err)
		return []ssh.Signer{}
	}
	return signers
}

func getCachedAuth(keyPath string) ssh.AuthMethod {
	if am, ok := authMethodCache.Load(keyPath); ok {
		switch authM := am.(type) {
		case ssh.AuthMethod:
			return authM
		case error:
			log.Tracef("already discarded key before %s: %v", keyPath, authM)
		default:
			log.Tracef("unexpected type %T for cached auth method for %s", am, keyPath)
		}
	}
	return nil
}

func (c *SSH) clientConfig() (*ssh.ClientConfig, error) {
	config := &ssh.ClientConfig{
		User: c.User,
	}

	hkc, err := c.hostkeyCallback()
	if err != nil {
		return nil, err
	}
	config.HostKeyCallback = hkc

	signers := c.signers()

	nonDefaultPaths := c.nonDefaultKeypaths()

	for _, keyPath := range c.keyPaths {
		if am := getCachedAuth(keyPath); am != nil {
			log.Tracef("%s: using a cached auth method for identity file %s", c, keyPath)
			config.Auth = append(config.Auth, am)
			continue
		}
		keyAuth, err := c.keyfileAuth(signers, keyPath)
		if err != nil {
			authMethodCache.Store(keyPath, err)
			// store the error so this key won't be loaded again

			if c.KeyPath != nil {
				return nil, ErrCantConnect.Wrapf("can't use configured identity file %s: %w", *c.KeyPath, err)
			}

			// if the key isn't one of the default paths, assume it was explicitly set, and
			// treat this as a fatal error
			for _, p := range nonDefaultPaths {
				if p == keyPath {
					return nil, ErrCantConnect.Wrapf("can't use identity file %s: %w", keyPath, err)
				}
			}

			log.Debugf("%s: failed to obtain a signer for identity file %s: %v", c, keyPath, err)
			continue
		}
		authMethodCache.Store(keyPath, keyAuth)
		config.Auth = append(config.Auth, keyAuth)
	}

	if len(config.Auth) == 0 {
		if len(signers) == 0 {
			return nil, ErrCantConnect.Wrapf("no usable authentication method found")
		}
		log.Debugf("%s: using all keys (%d) from ssh agent because a keypath was not explicitly given", c, len(signers))
		config.Auth = append(config.Auth, ssh.PublicKeys(signers...))
	}

	return config, nil
}

func (c *SSH) connectViaBastion(dst string, cfg *ssh.ClientConfig) error {
	if err := c.Bastion.Connect(); err != nil {
		if errors.Is(err, hostkey.ErrHostKeyMismatch) {
			return ErrCantConnect.Wrapf("bastion connect: %w", err)
		}
		return err
	}
	bconn, err := c.Bastion.client.Dial("tcp", dst)
	if err != nil {
		return fmt.Errorf("bastion dial: %w", err)
	}
	client, chans, reqs, err := ssh.NewClientConn(bconn, dst, cfg)
	if err != nil {
		if errors.Is(err, hostkey.ErrHostKeyMismatch) {
			return ErrCantConnect.Wrapf("bastion client connect: %w", err)
		}
		return fmt.Errorf("bastion client connect: %w", err)
	}
	c.client = ssh.NewClient(client, chans, reqs)

	return nil
}

// Connect opens the SSH connection
func (c *SSH) Connect() error {
	if err := defaults.Set(c); err != nil {
		return ErrValidationFailed.Wrapf("set defaults: %w", err)
	}

	config, err := c.clientConfig()
	if err != nil {
		return ErrCantConnect.Wrapf("create config: %w", err)
	}

	var port string
	if c.Port == nil {
		port = "22"
	} else {
		port = strconv.Itoa(*c.Port)
	}
	dst := net.JoinHostPort(c.connAddr, port)
	log.Debugf("%s: connecting to %s", c, dst)

	if c.Bastion != nil {
		return c.connectViaBastion(dst, config)
	}

	clientDirect, err := ssh.Dial("tcp", dst, config)
	if err != nil {
		if errors.Is(err, hostkey.ErrHostKeyMismatch) {
			return ErrCantConnect.Wrap(err)
		}
		return fmt.Errorf("ssh dial: %w", err)
	}
	c.client = clientDirect
	return nil
}

// pubkeysigner returns an ssh.AuthMethod for an ssh public key from the supplied signers
func (c *SSH) pubkeyAuth(signers []ssh.Signer, key ssh.PublicKey) (ssh.AuthMethod, error) {
	if len(signers) == 0 {
		return nil, ErrCantConnect.Wrapf("signer not found for public key")
	}

	keyM := key.Marshal()
	for _, s := range signers {
		if bytes.Equal(keyM, s.PublicKey().Marshal()) {
			log.Debugf("%s: signer for public key available in ssh agent", c)
			return ssh.PublicKeys(s), nil
		}
	}

	return nil, ErrAuthFailed.Wrapf("the provided key is a public key and is not known by agent")
}

// keyfileAuth returns an AuthMethod for the given keyPath. the signers are passed around to avoid querying them from agent multiple times
func (c *SSH) keyfileAuth(signers []ssh.Signer, path string) (ssh.AuthMethod, error) {
	log.Tracef("%s: checking identity file %s", c, path)
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, ErrCantConnect.Wrapf("read identity file: %w", err)
	}

	pubKey, _, _, _, err := ssh.ParseAuthorizedKey(key)
	if err == nil {
		log.Debugf("%s: file %s is a public key", c, path)
		return c.pubkeyAuth(signers, pubKey)
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
			if signer, err := c.keyfileAuth(signers, path+".pub"); err == nil {
				return signer, nil
			}
		}

		if sshConfig.Get(c.Address, "BatchMode") == "yes" {
			return nil, ErrCantConnect.Wrapf("passphrase required for encrypted key but BatchMode is set for host in ssh config: %w", err)
		}

		if c.PasswordCallback != nil {
			log.Tracef("%s: asking for a password to decrypt %s", c, path)
			pass, err := c.PasswordCallback()
			if err != nil {
				return nil, ErrCantConnect.Wrapf("password provider failed")
			}
			signer, err := ssh.ParsePrivateKeyWithPassphrase(key, []byte(pass))
			if err != nil {
				return nil, ErrCantConnect.Wrapf("protected key decoding failed: %w", err)
			}
			return ssh.PublicKeys(signer), nil
		}
	}

	return nil, ErrCantConnect.Wrapf("can't parse keyfile %s: %w", path, err)
}

const (
	ptyWidth  = 80
	ptyHeight = 40
)

// ExecStreams executes a command on the remote host and uses the passed in streams for stdin, stdout and stderr. It returns a Waiter with a .Wait() function that
// blocks until the command finishes and returns an error if the exit code is not zero.
func (c *SSH) ExecStreams(cmd string, stdin io.ReadCloser, stdout, stderr io.Writer, opts ...exec.Option) (Waiter, error) {
	if c.client == nil {
		return nil, ErrNotConnected
	}

	execOpts := exec.Build(opts...)
	cmd, err := execOpts.Command(cmd)
	if err != nil {
		return nil, ErrCommandFailed.Wrapf("build command: %w", err)
	}

	session, err := c.client.NewSession()
	if err != nil {
		return nil, ErrCantConnect.Wrapf("session: %w", err)
	}

	session.Stdin = stdin
	session.Stdout = stdout
	session.Stderr = stderr

	if err := session.Start(cmd); err != nil {
		return nil, ErrCantConnect.Wrapf("start: %w", err)
	}

	return session, nil
}

// Exec executes a command on the host
func (c *SSH) Exec(cmd string, opts ...exec.Option) error { //nolint:funlen,cyclop
	execOpts := exec.Build(opts...)
	session, err := c.client.NewSession()
	if err != nil {
		return fmt.Errorf("ssh new session: %w", err)
	}
	defer session.Close()

	cmd, err = execOpts.Command(cmd)
	if err != nil {
		return fmt.Errorf("build command: %w", err)
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
		return fmt.Errorf("ssh session start: %w", err)
	}

	if len(execOpts.Stdin) > 0 {
		execOpts.LogStdin(c.String())
		if _, err := io.WriteString(stdin, execOpts.Stdin); err != nil {
			return fmt.Errorf("write stdin: %w", err)
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

	gotErrors := false

	wg.Add(1)
	go func() {
		defer wg.Done()
		outputScanner := bufio.NewScanner(stderr)

		for outputScanner.Scan() {
			gotErrors = true
			execOpts.AddOutput(c.String(), "", outputScanner.Text()+"\n")
		}

		if err := outputScanner.Err(); err != nil {
			gotErrors = true
			execOpts.LogErrorf("%s: %s", c, err.Error())
		}
	}()

	err = session.Wait()
	wg.Wait()

	if err != nil {
		return fmt.Errorf("ssh session wait: %w", err)
	}

	if c.knowOs && c.isWindows && (!execOpts.AllowWinStderr && gotErrors) {
		return ErrCommandFailed.Wrapf("data in stderr")
	}

	return nil
}

// ExecInteractive executes a command on the host and copies stdin/stdout/stderr from local host
func (c *SSH) ExecInteractive(cmd string) error {
	session, err := c.client.NewSession()
	if err != nil {
		return fmt.Errorf("ssh new session: %w", err)
	}
	defer session.Close()

	session.Stdout = os.Stdout
	session.Stderr = os.Stderr

	fd := int(os.Stdin.Fd())
	old, err := term.MakeRaw(fd)
	if err != nil {
		return ErrOS.Wrapf("make terminal raw: %w", err)
	}

	defer func(fd int, old *term.State) {
		_ = term.Restore(fd, old)
	}(fd, old)

	rows, cols, err := term.GetSize(fd)
	if err != nil {
		return ErrOS.Wrapf("get terminal size: %w", err)
	}

	modes := ssh.TerminalModes{ssh.ECHO: 1}
	err = session.RequestPty("xterm", cols, rows, modes)
	if err != nil {
		return fmt.Errorf("request pty: %w", err)
	}

	stdinpipe, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("get stdin pipe: %w", err)
	}
	go func() {
		_, _ = io.Copy(stdinpipe, os.Stdin)
	}()

	captureSignals(stdinpipe, session)

	if cmd == "" {
		err = session.Shell()
	} else {
		err = session.Start(cmd)
	}

	if err != nil {
		return fmt.Errorf("ssh session: %w", err)
	}

	if err := session.Wait(); err != nil {
		return fmt.Errorf("ssh session wait: %w", err)
	}

	return nil
}
