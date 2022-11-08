package rig

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/kevinburke/ssh_config"
	ssh "golang.org/x/crypto/ssh"

	"golang.org/x/term"

	"github.com/acarl005/stripansi"
	"github.com/alessio/shellescape"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/log"
	ps "github.com/k0sproject/rig/powershell"

	"github.com/mitchellh/go-homedir"
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

type PasswordCallback func() (secret string, err error)

var defaultKeypaths = []string{"~/.ssh/id_rsa", "~/.ssh/identity", "~/.ssh/id_dsa"}

func (c *SSH) expandKeypath(path string) (string, bool) {
	expanded, err := homedir.Expand(path)
	if err != nil {
		return "", false
	}
	_, err = os.Stat(expanded)
	if err != nil {
		log.Debugf("%s: identity file %s not found", c, expanded)
		return "", false
	}
	log.Debugf("%s: found identity file %s", c, expanded)
	return expanded, true
}

func (c *SSH) keypathsFromConfig() []string {
	log.Debugf("%s: trying to get a keyfile path from ssh config", c)
	if idf := c.getConfigAll("IdentityFile"); len(idf) > 0 {
		log.Debugf("%s: detected %d identity file paths from ssh config", c, len(idf))
		return idf
	}
	return []string{}
}

// SetDefaults sets various default values
func (c *SSH) SetDefaults() {
	c.once.Do(func() {
		if c.KeyPath != nil && *c.KeyPath == "" {
			c.KeyPath = nil
		}
		if c.KeyPath != nil {
			if expanded, ok := c.expandKeypath(*c.KeyPath); ok {
				c.keyPaths = append(c.keyPaths, expanded)
			}
			return
		}

		paths := c.keypathsFromConfig()
		if len(paths) == 0 {
			paths = append(paths, defaultKeypaths...)
		}

		for _, p := range paths {
			if expanded, ok := c.expandKeypath(p); ok {
				log.Debugf("%s: using identity file %s", c, expanded)
				c.keyPaths = append(c.keyPaths, expanded)
			}
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

// create human-readable SSH-key strings
func keyString(k ssh.PublicKey) string {
	return k.Type() + " " + base64.StdEncoding.EncodeToString(k.Marshal()) // e.g. "ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTY...."
}

func trustedHostKeyCallback(trustedKey string) ssh.HostKeyCallback {
	return func(_ string, _ net.Addr, k ssh.PublicKey) error {
		ks := keyString(k)
		if trustedKey != ks {
			return fmt.Errorf("SSH host key verification failed")
		}

		return nil
	}
}

const hopefullyNonexistentHost = "thisH0stDoe5not3xist"

// Connect opens the SSH connection
func (c *SSH) Connect() error {
	config := &ssh.ClientConfig{
		User: c.User,
	}

	if c.HostKey == "" {
		config.HostKeyCallback = ssh.InsecureIgnoreHostKey()
	} else {
		config.HostKeyCallback = trustedHostKeyCallback(c.HostKey)
	}

	var signers []ssh.Signer
	agent, err := agentClient()
	if err != nil {
		log.Debugf("%s: failed to get ssh agent client: %v", c, err)
	} else {
		signers, err = agent.Signers()
		if err != nil {
			log.Debugf("%s: failed to get signers from ssh agent: %v", c, err)
		}
	}

	for _, keyPath := range c.keyPaths {
		privateKeyAuth, err := c.pkeySigner(signers, keyPath)
		if err != nil {
			log.Debugf("%s: failed to get a signer for %s: %v", c, keyPath, err)
			continue
		}

		config.Auth = append(config.Auth, privateKeyAuth)
	}

	if c.KeyPath == nil {
		dummyHostIdentityFiles := SSHConfigGetAll(hopefullyNonexistentHost, "IdentityFile")
		var expandedDummyIDFs []string
		for _, keyPath := range dummyHostIdentityFiles {
			if expanded, ok := c.expandKeypath(keyPath); ok {
				expandedDummyIDFs = append(expandedDummyIDFs, expanded)
			}
		}

		allDefault := true
		for _, keyPath := range c.keyPaths {
			found := false
			for _, idf := range expandedDummyIDFs {
				if idf == keyPath {
					found = true
					break
				}
			}
			if !found {
				allDefault = false
				break
			}
		}

		if allDefault && len(signers) > 0 {
			log.Debugf("%s: additionally using all keys (%d) from ssh agent because keypath was not explicitly given", c, len(signers))
			config.Auth = append(config.Auth, ssh.PublicKeys(signers...))
		}
	}

	dst := net.JoinHostPort(c.Address, strconv.Itoa(c.Port))

	var client *ssh.Client

	if c.Bastion == nil {
		clientDirect, err := ssh.Dial("tcp", dst, config)
		if err != nil {
			return err
		}
		client = clientDirect
	} else {
		if err := c.Bastion.Connect(); err != nil {
			return err
		}
		bconn, err := c.Bastion.client.Dial("tcp", dst)
		if err != nil {
			return err
		}
		c, chans, reqs, err := ssh.NewClientConn(bconn, dst, config)
		if err != nil {
			return err
		}
		client = ssh.NewClient(c, chans, reqs)
	}

	c.client = client
	return nil
}

func (c *SSH) pkeySigner(signers []ssh.Signer, path string) (ssh.AuthMethod, error) {
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	pubKey, _, _, _, err := ssh.ParseAuthorizedKey(key)
	if err == nil {
		log.Debugf("%s: key is a public key", c)

		for _, s := range signers {
			if bytes.Equal(pubKey.Marshal(), s.PublicKey().Marshal()) {
				log.Debugf("%s: using ssh agent signer for %s", c, path)
				return ssh.PublicKeys(s), nil
			}
		}

		if len(signers) > 0 {
			return nil, fmt.Errorf("the provided key %s is a public key and not known by agent", path)
		}
		return nil, fmt.Errorf("the provided key %s is a public key", path)
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err == nil {
		return ssh.PublicKeys(signer), nil
	}

	if _, ok := err.(*ssh.PassphraseMissingError); ok && c.PasswordCallback != nil {
		auth := ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
			pass, err := c.PasswordCallback()
			if err != nil {
				return nil, fmt.Errorf("password provider failed: %s", err)
			}
			signer, err := ssh.ParsePrivateKeyWithPassphrase(key, []byte(pass))
			if err != nil {
				return nil, err
			}
			return []ssh.Signer{signer}, nil
		})
		return auth, nil
	}

	return nil, fmt.Errorf("can't parse keyfile %s: %w", path, err)
}

// Exec executes a command on the host
func (c *SSH) Exec(cmd string, opts ...exec.Option) error {
	o := exec.Build(opts...)
	session, err := c.client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	cmd, err = o.Command(cmd)
	if err != nil {
		return err
	}

	if len(o.Stdin) == 0 && c.knowOs && !c.isWindows {
		// Only request a PTY when there's no STDIN data, because
		// then you would need to send a CTRL-D after input to signal
		// the end of text
		modes := ssh.TerminalModes{ssh.ECHO: 0}
		err = session.RequestPty("xterm", 80, 40, modes)
		if err != nil {
			return err
		}
	}

	o.LogCmd(c.String(), cmd)

	stdin, _ := session.StdinPipe()
	stdout, _ := session.StdoutPipe()
	stderr, _ := session.StderrPipe()

	if err := session.Start(cmd); err != nil {
		return err
	}

	if len(o.Stdin) > 0 {
		o.LogStdin(c.String())
		if _, err := io.WriteString(stdin, o.Stdin); err != nil {
			return err
		}
	}
	stdin.Close()

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		if o.Writer == nil {
			outputScanner := bufio.NewScanner(stdout)

			for outputScanner.Scan() {
				text := outputScanner.Text()
				stripped := stripansi.Strip(text)
				o.AddOutput(c.String(), stripped+"\n", "")
			}

			if err := outputScanner.Err(); err != nil {
				o.LogErrorf("%s: %s", c, err.Error())
			}
		} else {
			if _, err := io.Copy(o.Writer, stdout); err != nil {
				o.LogErrorf("%s: failed to stream stdout", c, err.Error())
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
			o.AddOutput(c.String(), "", outputScanner.Text()+"\n")
		}

		if err := outputScanner.Err(); err != nil {
			gotErrors = true
			o.LogErrorf("%s: %s", c, err.Error())
		}
	}()

	err = session.Wait()
	wg.Wait()

	if err != nil {
		return err
	}

	if c.knowOs && c.isWindows && (!o.AllowWinStderr && gotErrors) {
		return fmt.Errorf("command failed (received output to stderr on windows)")
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
		return err
	}
	defer session.Close()

	session.Stdout = os.Stdout
	session.Stderr = os.Stderr

	fd := int(os.Stdin.Fd())
	old, err := term.MakeRaw(fd)
	if err != nil {
		return err
	}

	defer func(fd int, old *term.State) {
		_ = term.Restore(fd, old)
	}(fd, old)

	rows, cols, err := term.GetSize(fd)
	if err != nil {
		return err
	}

	modes := ssh.TerminalModes{ssh.ECHO: 1}
	err = session.RequestPty("xterm", cols, rows, modes)
	if err != nil {
		return err
	}

	stdinpipe, err := session.StdinPipe()
	if err != nil {
		return err
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
		return err
	}

	return session.Wait()
}

func (c *SSH) uploadLinux(src, dst string, opts ...exec.Option) error {
	var err error
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	defer func() {
		if err != nil {
			log.Debugf("%s: cleaning up %s", c, dst)
			_ = c.Exec(fmt.Sprintf("rm -f -- %s", shellescape.Quote(dst)), opts...)
		}
	}()

	session, err := c.client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	hostIn, err := session.StdinPipe()
	if err != nil {
		return err
	}

	stderr, err := session.StderrPipe()
	if err != nil {
		return err
	}

	gw, err := gzip.NewWriterLevel(hostIn, gzip.BestSpeed)
	if err != nil {
		return err
	}

	o := exec.Build(opts...)
	teeCmd, err := o.Command(fmt.Sprintf("tee -- %s > /dev/null", shellescape.Quote(dst)))
	if err != nil {
		return err
	}
	unzipCmd := fmt.Sprintf("gzip -d | %s", teeCmd)
	log.Debugf("%s: executing `%s`", c, unzipCmd)

	err = session.Start(unzipCmd)
	if err != nil {
		return err
	}

	if _, err := io.Copy(gw, in); err != nil {
		return err
	}
	gw.Close()
	hostIn.Close()

	if err = session.Wait(); err != nil {
		msg, readErr := io.ReadAll(stderr)
		if readErr != nil {
			msg = []byte(readErr.Error())
		}

		return fmt.Errorf("upload failed: %s (%s)", err.Error(), msg)
	}

	return nil
}

func (c *SSH) uploadWindows(src, dst string, opts ...exec.Option) error {
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
		return err
	}
	sha256DigestLocalObj := sha256.New()
	sha256DigestLocal := ""
	sha256DigestRemote := ""
	srcSize := uint64(stat.Size())
	var bytesSent uint64
	var realSent uint64
	var fdClosed bool
	fd, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		if !fdClosed {
			_ = fd.Close()
			fdClosed = true
		}
	}()
	session, err := c.client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	hostIn, err := session.StdinPipe()
	if err != nil {
		return err
	}
	hostOut, err := session.StdoutPipe()
	if err != nil {
		return err
	}
	hostErr, err := session.StderrPipe()
	if err != nil {
		return err
	}

	o := exec.Build(opts...)
	psRunCmd, err := o.Command("powershell -ExecutionPolicy Unrestricted -EncodedCommand " + psCmd)
	if err != nil {
		return err
	}
	log.Debugf("%s: executing the upload command", c)
	if err := session.Start(psRunCmd); err != nil {
		return err
	}

	bufferCapacity := 262143 // use 256kb chunks
	base64LineBufferCapacity := bufferCapacity/3*4 + 2
	base64LineBuffer := make([]byte, base64LineBufferCapacity)
	base64LineBuffer[base64LineBufferCapacity-2] = '\r'
	base64LineBuffer[base64LineBufferCapacity-1] = '\n'
	buffer := make([]byte, bufferCapacity)
	var bufferLength int

	var ended bool

	for {
		var n int
		n, err = fd.Read(buffer)
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
				return err
			}
		}
	}
	_ = fd.Close()
	fdClosed = true
	if err == io.EOF {
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
				return err
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
		return fmt.Errorf("%s: upload failed: %s", c, err.Error())
	}

	wg.Wait()

	if sha256DigestRemote == "" {
		return fmt.Errorf("copy file command did not output the expected JSON to stdout but exited with code 0")
	} else if sha256DigestRemote != sha256DigestLocal {
		return fmt.Errorf("copy file checksum mismatch (local = %s, remote = %s)", sha256DigestLocal, sha256DigestRemote)
	}

	return nil
}
