package rig

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/acarl005/stripansi"
	"github.com/alessio/shellescape"
	"github.com/creasty/defaults"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/log"
	ps "github.com/k0sproject/rig/powershell"
	"github.com/kevinburke/ssh_config"
	ssh "golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

// SSH describes an SSH connection
type SSH struct {
	Address          string           `yaml:"address" validate:"required,hostname|ip"`
	User             string           `yaml:"user" validate:"required" default:"root"`
	Port             int              `yaml:"port" default:"22" validate:"gt=0,lte=65535"`
	KeyPath          *string          `yaml:"keyPath" validate:"omitempty"`
	HostKey          string           `yaml:"hostKey,omitempty"`
	Bastion          *SSH             `yaml:"bastion,omitempty"`
	PasswordCallback PasswordCallback `yaml:"-"`
	name             string

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
	defaultKeypaths   = []string{"~/.ssh/id_rsa", "~/.ssh/identity", "~/.ssh/id_dsa"}
	dummyhostKeyPaths []string
	globalOnce        sync.Once

	// ErrNoSignerFound is returned when no signer is found for a key
	ErrNoSignerFound = errors.New("no signer found for key")

	// ErrDataInStderr is returned when data is received for stderr from a windows host
	ErrDataInStderr = errors.New("command failed (received output to stderr on windows)")

	// ErrUnexpectedCopyOutput is returned when the output of a copy command is not as expected
	ErrUnexpectedCopyOutput = errors.New("copy command did not output the expected JSON")

	// ErrCopyFileChecksumMismatch is returned when the checksum of the uploaded file does not match the source file
	ErrCopyFileChecksumMismatch = errors.New("copy file checksum mismatch")

	// ErrInvalidPath is returned for paths that are invalid or unusable
	ErrInvalidPath = errors.New("invalid path")
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
		return "", fmt.Errorf("%w: ~user/ style paths not supported", ErrInvalidPath)
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
		return "", fmt.Errorf("%w: %v", ErrInvalidPath, err)
	}

	if stat.IsDir() {
		return "", fmt.Errorf("%w: %s is a directory", ErrInvalidPath, path)
	}

	return path, nil
}

func (c *SSH) keypathsFromConfig() []string {
	log.Tracef("%s: trying to get a keyfile path from ssh config", c)
	if idf := c.getConfigAll("IdentityFile"); len(idf) > 0 {
		log.Tracef("%s: detected %d identity file paths from ssh config: %v", c, len(idf), idf)
		return idf
	}
	log.Tracef("%s: no identity file paths found in ssh config", c)
	return []string{}
}

func (c *SSH) initGlobalDefaults() {
	log.Tracef("discovering global default keypaths")
	dummyHostIdentityFiles := SSHConfigGetAll(hopefullyNonexistentHost, "IdentityFile")
	for _, keyPath := range dummyHostIdentityFiles {
		if expanded, err := expandAndValidatePath(keyPath); err != nil {
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
		if len(paths) == 0 {
			// no paths found in ssh config either, use defaults
			paths = append(paths, defaultKeypaths...)
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

// try with port, if no results, try without
func (c *SSH) getConfigAll(key string) []string {
	dst := net.JoinHostPort(c.Address, strconv.Itoa(c.Port))
	if val := SSHConfigGetAll(dst, key); len(val) > 0 {
		return val
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
		c.isWindows = c.Exec("cmd.exe /c exit 0") == nil
		log.Debugf("%s: host is windows: %t", c, c.isWindows)
		c.knowOs = true
	}

	return c.isWindows
}

func (c *SSH) hostkeyCallback() (ssh.HostKeyCallback, error) { //nolint:unparam
	return ssh.InsecureIgnoreHostKey(), nil //nolint:gosec
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

	var signers []ssh.Signer
	agent, err := agentClient()
	if err != nil {
		log.Tracef("%s: failed to get ssh agent client: %v", c, err)
	} else {
		signers, err = agent.Signers()
		if err != nil {
			log.Debugf("%s: failed to list signers from ssh agent: %v", c, err)
		}
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

	if len(config.Auth) == 0 && len(signers) > 0 {
		log.Debugf("%s: using all keys (%d) from ssh agent because a keypath was not explicitly given", c, len(signers))
		config.Auth = append(config.Auth, ssh.PublicKeys(signers...))
	}

	return config, nil
}

// Connect opens the SSH connection
func (c *SSH) Connect() error {
	_ = defaults.Set(c)

	config, err := c.clientConfig()
	if err != nil {
		return err
	}

	dst := net.JoinHostPort(c.Address, strconv.Itoa(c.Port))

	if c.Bastion == nil {
		clientDirect, err := ssh.Dial("tcp", dst, config)
		if err != nil {
			return fmt.Errorf("ssh dial: %w", err)
		}
		c.client = clientDirect
		return nil
	}

	if err := c.Bastion.Connect(); err != nil {
		return err
	}
	bconn, err := c.Bastion.client.Dial("tcp", dst)
	if err != nil {
		return fmt.Errorf("bastion dial: %w", err)
	}
	client, chans, reqs, err := ssh.NewClientConn(bconn, dst, config)
	if err != nil {
		return fmt.Errorf("bastion client connect: %w", err)
	}
	c.client = ssh.NewClient(client, chans, reqs)

	return nil
}

func (c *SSH) pubkeySigner(signers []ssh.Signer, key ssh.PublicKey) (ssh.AuthMethod, error) {
	if len(signers) == 0 {
		return nil, fmt.Errorf("signer not found for public key: %w", ErrNoSignerFound)
	}

	for _, s := range signers {
		if bytes.Equal(key.Marshal(), s.PublicKey().Marshal()) {
			log.Debugf("%s: signer for public key available in ssh agent", c)
			return ssh.PublicKeys(s), nil
		}
	}

	return nil, fmt.Errorf("the provided key is a public key and is not known by agent: %w", ErrNoSignerFound)
}

func (c *SSH) pkeySigner(signers []ssh.Signer, path string) (ssh.AuthMethod, error) {
	log.Tracef("%s: checking identity file %s", c, path)
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read identity file: %w", err)
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
				return nil, fmt.Errorf("password provider failed: %w", err)
			}
			signer, err := ssh.ParsePrivateKeyWithPassphrase(key, []byte(pass))
			if err != nil {
				return nil, fmt.Errorf("protected key decoding failed: %w", err)
			}
			return ssh.PublicKeys(signer), nil
		}
	}

	return nil, fmt.Errorf("can't parse keyfile %s: %w", path, err)
}

const (
	ptyWidth  = 80
	ptyHeight = 40
)

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
		return ErrDataInStderr
	}

	return nil
}

// Upload uploads a file from local src path to remote dst
func (c *SSH) Upload(src, dst string, opts ...exec.Option) error {
	if c.IsWindows() {
		return c.uploadWindows(src, dst, opts...)
	}
	return c.uploadLinux(src, dst, opts...)
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
		return fmt.Errorf("make terminal raw: %w", err)
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

func (c *SSH) uploadLinux(src, dst string, opts ...exec.Option) error {
	var err error
	inFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open file for upload: %w", err)
	}
	defer inFile.Close()

	defer func() {
		if err != nil {
			log.Debugf("%s: cleaning up %s", c, dst)
			_ = c.Exec(fmt.Sprintf("rm -f -- %s", shellescape.Quote(dst)), opts...)
		}
	}()

	session, err := c.client.NewSession()
	if err != nil {
		return fmt.Errorf("ssh new session: %w", err)
	}
	defer session.Close()

	hostIn, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("get stdin pipe: %w", err)
	}

	stderr, err := session.StderrPipe()
	if err != nil {
		return fmt.Errorf("get stderr pipe: %w", err)
	}

	gzWriter, err := gzip.NewWriterLevel(hostIn, gzip.BestSpeed)
	if err != nil {
		return fmt.Errorf("create gzip writer: %w", err)
	}

	execOpts := exec.Build(opts...)
	teeCmd, err := execOpts.Command(fmt.Sprintf("tee -- %s > /dev/null", shellescape.Quote(dst)))
	if err != nil {
		return fmt.Errorf("build tee command: %w", err)
	}
	unzipCmd := fmt.Sprintf("gzip -d | %s", teeCmd)
	log.Debugf("%s: executing `%s`", c, unzipCmd)

	err = session.Start(unzipCmd)
	if err != nil {
		return fmt.Errorf("ssh session: %w", err)
	}

	if _, err := io.Copy(gzWriter, inFile); err != nil {
		return fmt.Errorf("copy file to remote: %w", err)
	}
	gzWriter.Close()
	hostIn.Close()

	if err = session.Wait(); err != nil {
		msg, readErr := io.ReadAll(stderr)
		if readErr != nil {
			msg = []byte(readErr.Error())
		}

		return fmt.Errorf("upload failed: %w (%s)", err, msg)
	}

	return nil
}

func (c *SSH) uploadWindows(src, dst string, opts ...exec.Option) error { //nolint:funlen,cyclop,gocognit
	var err error
	defer func() {
		if err != nil {
			log.Debugf("%s: cleaning up %s", c, dst)
			_ = c.Exec(fmt.Sprintf(`del %s`, ps.DoubleQuote(dst)), opts...)
		}
	}()
	psCmd := ps.UploadCmd(dst)
	stat, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat file for upload: %w", err)
	}
	sha256DigestLocalObj := sha256.New()
	sha256DigestLocal := ""
	sha256DigestRemote := ""
	srcSize := uint64(stat.Size())
	var bytesSent uint64
	var realSent uint64
	var fdClosed bool
	srcFd, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open file for upload: %w", err)
	}
	defer func() {
		if !fdClosed {
			_ = srcFd.Close()
			fdClosed = true
		}
	}()
	session, err := c.client.NewSession()
	if err != nil {
		return fmt.Errorf("ssh new session: %w", err)
	}
	defer session.Close()

	hostIn, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("get stdin pipe: %w", err)
	}
	hostOut, err := session.StdoutPipe()
	if err != nil {
		return fmt.Errorf("get stdout pipe: %w", err)
	}
	hostErr, err := session.StderrPipe()
	if err != nil {
		return fmt.Errorf("get stderr pipe: %w", err)
	}

	execOpts := exec.Build(opts...)
	psRunCmd, err := execOpts.Command("powershell -ExecutionPolicy Unrestricted -EncodedCommand " + psCmd)
	if err != nil {
		return fmt.Errorf("build powershell command: %w", err)
	}
	log.Debugf("%s: executing the upload command", c)
	if err := session.Start(psRunCmd); err != nil {
		return fmt.Errorf("ssh session: %w", err)
	}

	bufferCapacity := 262143                           // use 256kb chunks
	base64LineBufferCapacity := bufferCapacity/3*4 + 2 //nolint:gomnd
	base64LineBuffer := make([]byte, base64LineBufferCapacity)
	base64LineBuffer[base64LineBufferCapacity-2] = '\r'
	base64LineBuffer[base64LineBufferCapacity-1] = '\n'
	buffer := make([]byte, bufferCapacity)
	var bufferLength int

	var ended bool

	for {
		var n int
		n, err = srcFd.Read(buffer)
		bufferLength += n
		if err != nil {
			break
		}
		if bufferLength == bufferCapacity {
			base64.StdEncoding.Encode(base64LineBuffer, buffer)
			bytesSent += uint64(bufferLength)
			_, _ = sha256DigestLocalObj.Write(buffer)
			if bytesSent >= srcSize {
				ended = true
				sha256DigestLocal = hex.EncodeToString(sha256DigestLocalObj.Sum(nil))
			}
			b, err := hostIn.Write(base64LineBuffer)
			realSent += uint64(b)
			if ended {
				hostIn.Close()
			}

			bufferLength = 0
			if err != nil {
				return fmt.Errorf("write to remote: %w", err)
			}
		}
	}
	_ = srcFd.Close()
	fdClosed = true
	if errors.Is(err, io.EOF) {
		err = nil
	}
	if err != nil {
		return err
	}
	if !ended {
		_, _ = sha256DigestLocalObj.Write(buffer[:bufferLength])
		sha256DigestLocal = hex.EncodeToString(sha256DigestLocalObj.Sum(nil))
		base64.StdEncoding.Encode(base64LineBuffer, buffer[:bufferLength])
		i := base64.StdEncoding.EncodedLen(bufferLength)
		base64LineBuffer[i] = '\r'
		base64LineBuffer[i+1] = '\n'
		_, err = hostIn.Write(base64LineBuffer[:i+2])
		if err != nil {
			if !strings.Contains(err.Error(), ps.PipeHasEnded) && !strings.Contains(err.Error(), ps.PipeIsBeingClosed) {
				return fmt.Errorf("write to remote (tailing): %w", err)
			}
			// ignore pipe errors that results from passing true to cmd.SendInput
		}
		hostIn.Close()
	}
	var wg sync.WaitGroup
	var stderr bytes.Buffer
	var stdout bytes.Buffer

	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(&stderr, hostErr)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(hostOut)
		for scanner.Scan() {
			var output struct {
				Sha256 string `json:"sha256"`
			}
			if json.Unmarshal(scanner.Bytes(), &output) == nil {
				sha256DigestRemote = output.Sha256
			} else {
				_, _ = stdout.Write(scanner.Bytes())
				_, _ = stdout.WriteString("\n")
			}
		}
		if err := scanner.Err(); err != nil {
			stdout.Reset()
		}
	}()

	if err = session.Wait(); err != nil {
		return fmt.Errorf("%s: upload failed: %w", c, err)
	}

	wg.Wait()

	if sha256DigestRemote == "" {
		return ErrUnexpectedCopyOutput
	} else if sha256DigestRemote != sha256DigestLocal {
		return fmt.Errorf("%w (local = %s, remote = %s)", ErrCopyFileChecksumMismatch, sha256DigestLocal, sha256DigestRemote)
	}

	return nil
}
