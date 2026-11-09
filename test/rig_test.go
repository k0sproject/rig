package test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/k0sproject/rig/v2"
	"github.com/k0sproject/rig/v2/protocol"
	"github.com/k0sproject/rig/v2/protocol/localhost"
	"github.com/k0sproject/rig/v2/protocol/openssh"
	"github.com/k0sproject/rig/v2/protocol/ssh"
	"github.com/k0sproject/rig/v2/protocol/winrm"
	"github.com/k0sproject/rig/v2/remotefs"
	"github.com/k0sproject/rig/v2/rigtest"
	"github.com/k0sproject/rig/v2/sh/shellescape"
	"github.com/k0sproject/rig/v2/sshconfig"
	"github.com/k0sproject/rig/v2/stattime"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// Define variables directly
var (
	targetHost      string
	targetPort      int
	username        string
	proto           string
	keyPath         string
	configPath      string
	password        string
	useHTTPS        bool
	onlyConnect     bool
	privateKey      string
	enableMultiplex bool
	traceLog        bool
	expectSudo      bool
	expectSudoTTY   bool
)

func pathBase(p string) string {
	return path.Base(strings.ReplaceAll(p, "\\", "/"))
}

func pathDir(p string) string {
	return path.Dir(strings.ReplaceAll(p, "\\", "/"))
}

func TestMain(m *testing.M) {
	flag.StringVar(&targetHost, "host", "", "target host")
	flag.IntVar(&targetPort, "port", 22, "target host port (defaulted based on protocol)")
	flag.StringVar(&username, "user", "root", "user name")
	flag.StringVar(&proto, "protocol", "ssh", "ssh/winrm/localhost/openssh")
	flag.StringVar(&keyPath, "ssh-keypath", "", "ssh keypath")
	flag.StringVar(&configPath, "ssh-configpath", "", "ssh config path")
	flag.StringVar(&privateKey, "ssh-private-key", "", "ssh private key")
	flag.StringVar(&password, "winrm-password", "", "winrm password")
	flag.BoolVar(&useHTTPS, "winrm-https", false, "use https for winrm")
	flag.BoolVar(&enableMultiplex, "openssh-multiplex", true, "use ssh multiplexing")
	flag.BoolVar(&onlyConnect, "connect", false, "only connect to host, dont run other tests")
	flag.BoolVar(&expectSudo, "expect-sudo", false, "the host has passwordless privilege escalation configured, fail instead of skipping when rig does not detect it")
	flag.BoolVar(&expectSudoTTY, "expect-sudo-tty", false, "sudoers sets requiretty, so rig must fall back to sudo.TTY; fail instead of skipping the checks that depend on it")
	flag.BoolVar(&traceLog, "trace", false, "turn on trace logging")

	// Parse the flags
	flag.Parse()

	// A host where rig must fall back to sudo.TTY has sudo by definition, so
	// none of the escalation tests may skip for lack of it.
	if expectSudoTTY {
		expectSudo = true
	}

	if traceLog {
		rigtest.TraceToStderr()
	}

	if targetHost == "" {
		// no host, nothing to test
		return
	}

	if targetPort == 22 && proto == "winrm" {
		if useHTTPS {
			targetPort = 5986
		} else {
			targetPort = 5985
		}
	}

	if configPath != "" {
		f, err := os.Open(configPath)
		if err != nil {
			panic(err)
		}
		defer f.Close()
		parser, err := sshconfig.NewParser(f)
		if err != nil {
			panic(err)
		}
		ssh.ConfigParser = parser
	}

	// Run tests
	os.Exit(m.Run())
}

func TestConnect(t *testing.T) {
	if !onlyConnect {
		t.Skip("skip")
		return
	}

	h := GetHost(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	err := h.Connect(ctx)
	require.NoError(t, err)
	h.Disconnect()
}

func TestConfigurerSuite(t *testing.T) {
	if onlyConnect {
		t.Skip("only connect")
		return
	}
	suite.Run(t, &OSSuite{ConnectedSuite: ConnectedSuite{Host: GetHost(t)}})
}

func TestFSSuite(t *testing.T) {
	if onlyConnect {
		t.Skip("only connect")
		return
	}

	h := GetHost(t)
	t.Run("No sudo", func(t *testing.T) {
		suite.Run(t, &FSSuite{ConnectedSuite: ConnectedSuite{Host: h}})
	})

	t.Run("Sudo", func(t *testing.T) {
		suite.Run(t, &FSSuite{ConnectedSuite: ConnectedSuite{Host: h}, sudo: true})
	})
}

// Host is a host that utilizes rig for connections
type Host struct {
	*rig.Client
}

func GetHost(t *testing.T, options ...rig.ClientOption) *Host {
	var client protocol.Connection
	switch proto {
	case "ssh":
		cfg := ssh.Config{
			Address: targetHost,
			Port:    targetPort,
			User:    username,
		}

		if privateKey != "" {
			authM, err := ssh.ParseSSHPrivateKey([]byte(privateKey), ssh.DefaultPasswordCallback)
			if err != nil {
				panic(err)
			}
			cfg.AuthMethods = authM
		}

		if keyPath != "" {
			cfg.KeyPath = &keyPath
		}
		sshclient, err := ssh.NewConnection(cfg)
		require.NoError(t, err)
		client = sshclient
	case "winrm":
		cfg := winrm.Config{
			Address:  targetHost,
			Port:     targetPort,
			User:     username,
			UseHTTPS: useHTTPS,
			Insecure: true,
			Password: password,
		}
		winrmclient, err := winrm.NewConnection(cfg)
		require.NoError(t, err)
		client = winrmclient
	case "localhost":
		client, _ = localhost.NewConnection()
	case "openssh":
		cfg := openssh.Config{
			Address:             targetHost,
			DisableMultiplexing: !enableMultiplex,
		}
		if targetPort != 22 {
			cfg.Port = &targetPort
		}

		if keyPath != "" {
			cfg.KeyPath = &keyPath
		}
		if username != "" {
			cfg.User = &username
		}
		if configPath != "" {
			cfg.ConfigPath = &configPath
		}
		opensshclient, err := openssh.NewConnection(cfg)
		require.NoError(t, err)
		client = opensshclient
	default:
		panic("unknown protocol")
	}
	opts := []rig.ClientOption{rig.WithConnection(client), rig.WithLogger(slog.New(NewTestLogHandler(t)))}
	opts = append(opts, options...)
	c, err := rig.NewClient(opts...)
	require.NoError(t, err)
	return &Host{Client: c}
}

type SuiteLogger struct {
	t *testing.T
}

type TestLogHandler struct {
	T *testing.T
}

func (h *TestLogHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *TestLogHandler) WithAttrs(_ []slog.Attr) slog.Handler         { return h }
func (h *TestLogHandler) WithGroup(_ string) slog.Handler              { return h }
func (h *TestLogHandler) Handle(_ context.Context, r slog.Record) error {
	h.T.Helper()
	buf := &strings.Builder{}
	buf.WriteString(r.Level.String())
	buf.WriteByte(' ')
	buf.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool {
		buf.WriteByte(' ')
		buf.WriteString(a.Key)
		buf.WriteByte('=')
		buf.WriteString(fmt.Sprintf("%v", a.Value))
		return true
	})
	h.T.Log(buf.String())
	return nil
}

func NewTestLogHandler(t *testing.T) *TestLogHandler {
	return &TestLogHandler{T: t}
}

type ConnectedSuite struct {
	suite.Suite
	tempDir string
	count   int
	Host    *Host
	fs      remotefs.FS
	sudo    bool
}

func (s *ConnectedSuite) SetupSuite() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	err := s.Host.Connect(ctx)
	s.Require().NoError(err)
	if s.sudo {
		s.fs = s.Host.Sudo().FS()
	} else {
		s.fs = s.Host.FS()
	}
	tempDir, err := s.fs.MkdirTemp("", "rigtest")
	s.Require().NoError(err)
	s.tempDir = tempDir
}

func (s *ConnectedSuite) TearDownSuite() {
	if s.Host == nil {
		return
	}
	_ = s.Host.FS().RemoveAll(s.tempDir)
	s.Host.Disconnect()
}

func (s *ConnectedSuite) TempPath(args ...string) string {
	if len(args) == 0 {
		s.count++
		return fmt.Sprintf("%s/testfile.%d", s.tempDir, s.count)
	}
	args[0] = fmt.Sprintf("%s/%s", s.tempDir, args[0])
	return strings.Join(args, "/")
}

type OSSuite struct {
	ConnectedSuite
}

// TestSudoCompoundCommands guards against regressing the sudo decorator into handing
// compound shell expressions to sudo verbatim. v2 already wraps commands in
// `sudo -n -- /bin/sh -c -- <quoted>` (see sudo.Sudo), so this is a regression guard,
// not a bug fix. It only has teeth when the suite runs as a non-root user with passwordless
// escalation (the rig_test_regular_user paths in test.sh), which pass -expect-sudo so that
// escalation rig fails to detect is reported instead of quietly skipped.
func (s *OSSuite) TestSudoCompoundCommands() {
	if s.Host.IsWindows() {
		s.T().Skip("sudo not supported on windows")
		return
	}
	if _, err := s.Host.Sudo().ExecOutput("true"); err != nil {
		// Detection runs commands of its own, so a login shell that mangles them takes
		// escalation away without any test asserting on it. That is how the fish login
		// shell of k0sproject/k0sctl#1135 went unnoticed.
		s.Require().Falsef(expectSudo, "the host has passwordless privilege escalation configured but rig did not detect it: %v", err)
		s.T().Skip("passwordless privilege escalation (sudo/doas) not configured")
		return
	}

	s.Run("Pipeline elevates the whole expression", func() {
		// If only the first stage were wrapped, "id -u" would report the unprivileged
		// login user's uid instead of 0. The exit code stays 0, so the value must be
		// asserted, not just the error.
		out, err := s.Host.Sudo().ExecOutput("true | id -u")
		s.Require().NoError(err)
		s.Equal("0", strings.TrimSpace(out))
	})

	s.Run("Subshell group", func() {
		// Handing "(exit 1) || echo recovered" to sudo verbatim is a parse error near "("
		// that aborts the whole line before the "||" branch can run.
		out, err := s.Host.Sudo().ExecOutput("(exit 1) || echo recovered")
		s.Require().NoError(err)
		s.Equal("recovered", strings.TrimSpace(out))
	})
}

// TestSudoExitStatus checks that an elevated command's exit status, and a
// context ending while it runs, reach the caller through the sudo runner rig
// detected. Under requiretty that runner is sudo.TTY, whose exit status is
// relayed outside script(1), which reports 0 when it is interrupted.
//
// A context ending over native SSH asks the server to interrupt the command
// with a signal, so the elevated command and every process between it and the
// server must be gone afterwards whenever an unelevated command is. Under
// sudo.TTY that takes the wrapper and the shell script starts passing the
// signal on, as sudo runs in a session of its own. Not every server acts on
// the request: the OpenSSH 9 servers of Debian 12 and Ubuntu 24.04 leave even
// an unelevated command of a root login running, and the openssh protocol
// stops only the local ssh client. doas is not held to it either: it gives up
// the user's real uid before running the command, so the server, which sends
// the signal as the user, is not allowed to.
//
// With -expect-sudo-tty the command must run under script(1), and over native
// SSH the check that it stops must not be skipped.
func (s *OSSuite) TestSudoExitStatus() {
	if s.Host.IsWindows() {
		s.Require().False(expectSudoTTY, "sudo.TTY does not apply to windows")
		s.T().Skip("sudo not supported on windows")
		return
	}
	if _, err := s.Host.Sudo().ExecOutput("true"); err != nil {
		s.Require().Falsef(expectSudo, "the host has passwordless privilege escalation configured but rig did not detect it: %v", err)
		s.T().Skip("passwordless privilege escalation (sudo/doas) not configured")
		return
	}

	s.Run("Non-zero exit status", func() {
		err := s.Host.Sudo().Exec("exit 17")
		s.Require().Error(err)
		s.Equal(17, exitCodeOf(err), "error: %v", err)
	})

	s.Run("Context ends while the command runs", func() {
		if s.Host.Exec("test -r /proc/self/status") != nil {
			s.Require().False(expectSudoTTY, "following the processes of a sudo.TTY command needs Linux procfs")
			s.execCancelled(s.Host.Sudo(), "sleep 30")
			s.T().Skip("no Linux procfs to follow the command's processes with")
			return
		}
		sudoUser, err := s.Host.Sudo().ExecOutput(`echo "${SUDO_USER:-}"`)
		s.Require().NoError(err)
		stopsPlain, _ := s.cancelledCommandStops(s.Host.Client)
		stopsElevated, chain := s.cancelledCommandStops(s.Host.Sudo())
		if expectSudoTTY {
			s.Contains(chain, "script", "the elevated command did not run under script(1), so sudo.TTY was not used")
			if proto == "ssh" {
				s.Require().True(stopsPlain, "an unelevated command outlived its context, so the elevated one could not be checked")
			}
		}
		switch {
		case !stopsPlain:
			s.T().Log("an unelevated command outlives its context on this connection, so the elevated one is not required to stop")
		case sudoUser == "":
			s.T().Log("privileges are not escalated through sudo, so the elevated command is not required to stop")
		default:
			s.True(stopsElevated, "the elevated command or a process running it outlived its context, an unelevated one does not: %v", chain)
		}
	})
}

// cancelledCommandStops runs a command through client until a short context
// ends and reports whether the command and every process between it and the
// SSH server are gone afterwards, along with those processes' names, the
// command's first. The command must still fail with the context's error either
// way. Processes that outlive the context are killed. It needs Linux procfs.
func (s *OSSuite) cancelledCommandStops(client *rig.Client) (bool, []string) {
	chainFile := s.TempPath()
	// Each line is a pid and its name, walking up from the command until the
	// server, which is not recorded.
	record := `p=$$; while [ "$p" -gt 1 ]; do n=$(cat /proc/$p/comm); case $n in sshd*) break;; esac; ` +
		`echo "$p $n"; p=$(sed -n 's/^PPid:[[:space:]]*//p' /proc/$p/status); done > ` + shellescape.Quote(chainFile)
	s.execCancelled(client, record+"; exec sleep 30")

	out, err := client.ExecOutput("cat " + shellescape.Quote(chainFile))
	s.Require().NoError(err)
	var pids []int
	var names []string
	for line := range strings.Lines(out) {
		pidField, name, ok := strings.Cut(strings.TrimSpace(line), " ")
		s.Require().True(ok, "malformed process chain line %q", line)
		pid, err := strconv.Atoi(pidField)
		s.Require().NoError(err, "malformed process chain line %q", line)
		pids = append(pids, pid)
		names = append(names, name)
	}
	s.Require().NotEmpty(pids, "the command did not report its processes")

	// A zombie counts as gone: reaping it is up to the host's init.
	var gone strings.Builder
	for _, pid := range pids {
		fmt.Fprintf(&gone, "{ ! [ -e /proc/%[1]d ] || grep -q '^State:[[:space:]]*Z' /proc/%[1]d/status; } && ", pid)
	}
	gone.WriteString("true")
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if s.Host.Exec(gone.String()) == nil {
			return true, names
		}
	}
	// The command is first and killing it ends the rest; a process gone in
	// the meantime makes kill fail, which is fine.
	_ = client.Exec(fmt.Sprintf("kill %d", pids[0]))
	return false, names
}

// execCancelled runs command through client until a short context ends and
// checks that it fails with the context's error, promptly.
func (s *OSSuite) execCancelled(client *rig.Client, command string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	err := client.ExecContext(ctx, command)
	s.Require().ErrorIs(err, context.DeadlineExceeded, "an interrupted command must not be reported as a success")
	s.Less(time.Since(start), 20*time.Second)
}

// exitCodeOf returns the exit status err carries, from the native SSH
// protocol's exit error or from a local process one, or -1.
func exitCodeOf(err error) int {
	if withStatus, ok := errors.AsType[interface {
		error
		ExitStatus() int
	}](err); ok {
		return withStatus.ExitStatus()
	}
	if withCode, ok := errors.AsType[interface {
		error
		ExitCode() int
	}](err); ok {
		return withCode.ExitCode()
	}
	return -1
}

func (s *OSSuite) TestStat() {
	s.Run("File does not exist", func() {
		stat, err := s.fs.Stat(s.TempPath("doesnotexist"))
		s.Nil(stat)
		s.Error(err)
	})

	s.Run("File exists", func() {
		f := s.TempPath()
		s.Run("Create file", func() {
			s.Require().NoError(s.fs.Touch(f))
		})

		stat, err := s.fs.Stat(f)
		s.Require().NoError(err)
		s.True(strings.HasSuffix(f, stat.Name())) // Name() returns Basename
	})
}

func (s *OSSuite) TestTouch() {
	f := s.TempPath()
	s.Require().NoError(s.fs.Touch(f))
	now := time.Now()
	// The second round sets a timestamp the file does not already have, so that a
	// Chtimes that quietly leaves an existing file alone can't pass as a success.
	for _, tt := range []time.Time{now, now.Add(1 * time.Hour)} {
		s.Run("Update timestamp "+tt.String(), func() {
			s.Require().NoError(s.fs.Chtimes(f, tt.UnixNano(), tt.UnixNano()))
		})

		s.Run("File exists and has correct timestamp "+tt.String(), func() {
			stat, err := s.fs.Stat(f)
			s.Require().NoError(err)
			s.NotNil(stat)
			s.Equal(tt.Unix(), stat.ModTime().Unix())
			if s.Host.IsWindows() {
				s.T().Log("Testing millisecond precision on windows")
				s.Equal(tt.UnixMilli(), stat.ModTime().UnixMilli(), "expected %d (%s), got %d (%s)", tt.UnixMilli(), tt, stat.ModTime().UnixMilli(), stat.ModTime())
			} else if stat.ModTime().Nanosecond() != 0 {
				s.T().Log("Testing nanosecond precision")
				s.Equal(tt.UnixNano(), stat.ModTime().UnixNano())
			}
			s.True(stattime.Equal(tt, stat.ModTime()))
		})
	}
}

func (s *OSSuite) TestFileAccess() {
	f := s.TempPath()
	s.Run("File does not exist", func() {
		s.False(s.fs.FileExist(f))
	})

	s.Run("Write file", func() {
		s.Require().NoError(s.fs.WriteFile(f, []byte("test\ntest2\ntest3"), 0o644))
	})

	s.Run("File exists", func() {
		s.True(s.fs.FileExist(f))
	})

	s.Run("Read file and verify contents", func() {
		content, err := s.fs.ReadFile(f)
		s.Require().NoError(err)
		s.Equal("test\ntest2\ntest3", string(content))
	})

	s.Run("Delete file", func() {
		s.Require().NoError(s.fs.Remove(f))
	})

	s.Run("File does not exist", func() {
		s.False(s.fs.FileExist(f))
	})
}

func testFile(size int64) (string, error) {
	// Create a temporary file.
	file, err := os.CreateTemp("", "rigtest.*.dat")
	if err != nil {
		return "", err
	}
	defer file.Close()

	// Write random data to the file.
	_, err = io.CopyN(file, rand.Reader, size)
	if err != nil {
		return "", err
	}

	return file.Name(), nil
}

func (s *OSSuite) TestUpload() {
	for _, size := range []int64{500, 100 * 1024, 1024 * 1024} {
		s.Run(fmt.Sprintf("File size %d", size), func() {
			fn, err := testFile(size)
			s.Require().NoError(err)
			defer os.Remove(fn)
			defer s.fs.Remove(s.TempPath(pathBase(fn)))

			s.Run("Upload file", func() {
				s.Require().NoError(remotefs.Upload(s.Host.FS(), fn, s.TempPath(pathBase(fn))))
			})

			s.Run("Verify file size", func() {
				stat, err := s.fs.Stat(s.TempPath(pathBase(fn)))
				s.Require().NoError(err)
				s.Require().NotNil(stat)
				s.Equal(size, stat.Size())
			})

			s.Run("Verify file contents", func() {
				sum, err := s.fs.Sha256(s.TempPath(pathBase(fn)))
				s.Require().NoError(err)
				sha := sha256.New()
				f, err := os.Open(fn)
				s.Require().NoError(err)
				_, err = io.Copy(sha, f)
				s.Require().NoError(err)
				s.Equal(hex.EncodeToString(sha.Sum(nil)), sum)
			})

		})
	}
}

type FSSuite struct {
	ConnectedSuite
	sudo bool
}

func (s *FSSuite) TestMkdir() {
	testPath := s.TempPath("test", "subdir")
	defer func() {
		_ = s.fs.RemoveAll(testPath)
	}()
	s.Run("Create directory", func() {
		s.Require().NoError(s.fs.MkdirAll(testPath, 0700))
	})
	s.Run("Verify directory exists", func() {
		stat, err := s.fs.Stat(testPath)
		s.Require().NoError(err)
		s.Run("Check permissions", func() {
			if s.Host.IsWindows() {
				s.T().Skip("Windows does not support chmod permissions")
			}
			s.Equal(os.FileMode(0700), stat.Mode().Perm())
			parent, err := s.fs.Stat(s.TempPath("test"))
			s.Require().NoError(err)
			s.Equal(os.FileMode(0700), parent.Mode().Perm())
		})
	})
}

func (s *FSSuite) TestRemove() {
	testPath := s.TempPath("test", "subdir")
	s.Run("Create directory", func() {
		s.Require().NoError(s.fs.MkdirAll(testPath, 0755))
	})
	s.Run("Remove directory", func() {
		s.Require().NoError(s.fs.RemoveAll(testPath))
	})
	s.Run("Verify directory does not exist", func() {
		stat, err := s.fs.Stat(testPath)
		s.Nil(stat)
		s.Error(err)
		s.True(os.IsNotExist(err))
	})
	s.Run("Remove parent directory", func() {
		s.Require().NoError(s.fs.RemoveAll(s.TempPath("test")))
	})
	s.Run("Verify parent directory does not exist", func() {
		stat, err := s.fs.Stat(s.TempPath("test"))
		s.Nil(stat)
		s.Error(err)
		s.True(os.IsNotExist(err))
	})
}

func (s *FSSuite) TestReadWriteFile() {
	for _, testFileSize := range []int64{
		int64(500),           // less than one block on most filesystems
		int64(1 << (10 * 2)), // exactly 1MB
		int64(4096),          // exactly one block on most filesystems
		int64(4097),          // plus 1
	} {
		s.Run(fmt.Sprintf("File size %d", testFileSize), func() {
			fn := s.TempPath()

			origin := io.LimitReader(rand.Reader, testFileSize)
			shasum := sha256.New()
			reader := io.TeeReader(origin, shasum)

			defer func() {
				_ = s.fs.Remove(fn)
			}()
			s.Run("Write file", func() {
				f, err := s.fs.OpenFile(fn, os.O_CREATE|os.O_WRONLY, 0644)
				s.Require().NoError(err)
				n, err := io.Copy(f, reader)
				s.Require().NoError(err)
				s.Equal(testFileSize, n)
				s.Require().NoError(f.Close())
			})

			s.Run("Verify file size", func() {
				stat, err := s.fs.Stat(fn)
				s.Require().NoError(err)
				s.Equal(testFileSize, stat.Size())
			})

			s.Run("Verify file sha256", func() {
				sum, err := s.fs.Sha256(fn)
				s.Require().NoError(err)
				s.Equal(hex.EncodeToString(shasum.Sum(nil)), sum)
			})

			readSha := sha256.New()
			s.Run("Read file", func() {
				f, err := s.fs.Open(fn)
				s.Require().NoError(err)
				n, err := io.Copy(readSha, f)
				s.Require().NoError(err)
				s.Equal(testFileSize, n)
				s.Require().NoError(f.Close())
			})

			s.Run("Verify read file sha256", func() {
				s.Equal(shasum.Sum(nil), readSha.Sum(nil))
			})
		})
	}
}

func (s *FSSuite) TestReadWriteFileCopy() {
	for _, testFileSize := range []int64{
		int64(500),           // less than one block on most filesystems
		int64(1 << (10 * 2)), // exactly 1MB
		int64(4096),          // exactly one block on most filesystems
		int64(4097),          // plus 1
	} {
		s.Run(fmt.Sprintf("File size %d", testFileSize), func() {
			fn := s.TempPath()

			origin := io.LimitReader(rand.Reader, testFileSize)
			shasum := sha256.New()
			reader := io.TeeReader(origin, shasum)

			defer func() {
				_ = s.fs.Remove(fn)
			}()
			s.Run("Write file", func() {
				f, err := s.fs.OpenFile(fn, os.O_CREATE|os.O_WRONLY, 0644)
				s.Require().NoError(err)
				n, err := f.CopyFrom(reader)
				s.Require().NoError(err)
				s.Equal(testFileSize, n)
				s.Require().NoError(f.Close())
			})

			s.Run("Verify file size", func() {
				stat, err := s.fs.Stat(fn)
				s.Require().NoError(err)
				s.Equal(testFileSize, stat.Size())
			})

			s.Run("Verify file sha256", func() {
				sum, err := s.fs.Sha256(fn)
				s.Require().NoError(err)
				s.Equal(hex.EncodeToString(shasum.Sum(nil)), sum)
			})

			readSha := sha256.New()
			s.Run("Read file", func() {
				fsf, err := s.fs.Open(fn)
				s.Require().NoError(err)
				f, ok := fsf.(remotefs.File)
				s.Require().True(ok)
				n, err := f.CopyTo(readSha)
				s.Require().NoError(err)
				s.Equal(testFileSize, n)
				s.Require().NoError(f.Close())
			})

			s.Run("Verify read file sha256", func() {
				s.Equal(shasum.Sum(nil), readSha.Sum(nil))
			})
		})
	}
}

type RepeatReader struct {
	data []byte
}

func (r *RepeatReader) Read(p []byte) (n int, err error) {
	for i := range p {
		p[i] = r.data[i%len(r.data)]
	}
	return len(p), nil
}

func (s *FSSuite) TestSeek() {
	fn := s.TempPath()
	reference := bytes.Repeat([]byte{'a'}, 1024)
	defer func() {
		_ = s.fs.Remove(fn)
	}()
	f, err := s.fs.OpenFile(fn, os.O_CREATE|os.O_WRONLY, 0644)
	s.Require().NoError(err)
	n, err := io.Copy(f, bytes.NewReader(bytes.Repeat([]byte{'a'}, 1024)))
	s.Require().NoError(err)
	s.Equal(int64(1024), n)
	s.Require().NoError(f.Close())

	s.Run("Verify contents", func() {
		f, err := s.fs.Open(fn)
		s.Require().NoError(err)
		b, err := io.ReadAll(f)
		s.Require().NoError(err)
		s.Equal(1024, len(b))
		s.Require().NoError(f.Close())
		s.Equal(reference, b)
	})
	s.Run("Alter file beginning", func() {
		f, err := s.fs.OpenFile(fn, os.O_WRONLY, 0644)
		s.Require().NoError(err)
		np, err := f.Seek(0, io.SeekStart)
		s.Require().NoError(err)
		s.Equal(int64(0), np)
		n, err := io.Copy(f, bytes.NewReader(bytes.Repeat([]byte{'b'}, 256)))
		s.Require().NoError(err)
		s.Equal(int64(256), n)
		s.Require().NoError(f.Close())
	})
	copy(reference[0:256], bytes.Repeat([]byte{'b'}, 256))
	s.Run("Verify contents after file beginning altered", func() {
		f, err := s.fs.Open(fn)
		s.Require().NoError(err)
		b, err := io.ReadAll(f)
		s.Require().NoError(err)
		s.Equal(1024, len(b))
		s.Require().NoError(f.Close())
		s.Equal(reference, b)
	})
	s.Run("Alter file ending", func() {
		f, err := s.fs.OpenFile(fn, os.O_WRONLY, 0644)
		s.Require().NoError(err)
		np, err := f.Seek(-256, io.SeekEnd)
		s.Require().NoError(err)
		s.Equal(int64(768), np)
		n, err := io.Copy(f, bytes.NewReader(bytes.Repeat([]byte{'c'}, 256)))
		s.Require().NoError(err)
		s.Equal(int64(256), n)
		s.Require().NoError(f.Close())
	})
	copy(reference[768:1024], bytes.Repeat([]byte{'c'}, 256))
	s.Run("Verify contents after file ending altered", func() {
		f, err := s.fs.Open(fn)
		s.Require().NoError(err)
		b, err := io.ReadAll(f)
		s.Require().NoError(err)
		s.Equal(1024, len(b))
		s.Require().NoError(f.Close())
		s.Equal(reference, b)
	})
	s.Run("Alter file middle", func() {
		f, err := s.fs.OpenFile(fn, os.O_WRONLY, 0644)
		s.Require().NoError(err)
		np, err := f.Seek(256, io.SeekStart)
		s.Require().NoError(err)
		s.Equal(int64(256), np)
		n, err := io.Copy(f, bytes.NewReader(bytes.Repeat([]byte{'d'}, 512)))
		s.Require().NoError(err)
		s.Equal(int64(512), n)
		s.Require().NoError(f.Close())
	})
	copy(reference[256:768], bytes.Repeat([]byte{'d'}, 512))
	s.Run("Verify contents after file middle altered", func() {
		f, err := s.fs.Open(fn)
		s.Require().NoError(err)
		b, err := io.ReadAll(f)
		s.Require().NoError(err)
		s.Equal(1024, len(b))
		s.Require().NoError(f.Close())
		s.Equal(reference, b)
	})
}

func (s *FSSuite) TestReadDir() {
	defer func() {
		_ = s.fs.RemoveAll(s.TempPath("test"))
	}()
	s.Run("Create directory", func() {
		s.Require().NoError(s.fs.MkdirAll(s.TempPath("test"), 0755))
	})
	s.Run("Create files", func() {
		for _, fn := range []string{s.TempPath("test", "subdir", "nestedfile"), s.TempPath("test", "file")} {
			s.Require().NoError(s.fs.MkdirAll(pathDir(fn), 0755))
			f, err := s.fs.OpenFile(fn, os.O_CREATE|os.O_WRONLY, 0644)
			s.Require().NoError(err)
			n, err := f.Write([]byte("test"))
			s.Require().NoError(err)
			s.Equal(4, n)
			s.Require().NoError(f.Close())
		}
	})

	s.Run("Read directory", func() {
		dir, err := s.fs.OpenFile(s.TempPath("test"), os.O_RDONLY, 0644)
		s.Require().NoError(err)
		s.Require().NotNil(dir)
		readDirFile, ok := dir.(fs.ReadDirFile)
		s.Require().True(ok)
		entries, err := readDirFile.ReadDir(-1)
		s.Require().NoError(err)
		s.Require().Len(entries, 2)
		s.Equal("subdir", entries[0].Name())
		s.True(entries[0].IsDir())
		s.Equal("file", entries[1].Name())
		s.False(entries[1].IsDir())
		s.Require().NoError(dir.Close())
	})

	s.Run("Walkdir", func() {
		var entries []string
		s.Require().NoError(fs.WalkDir(s.fs, s.TempPath("test"), func(path string, d fs.DirEntry, err error) error {
			s.Require().NoError(err)
			info, err := d.Info()
			s.Require().NoError(err)
			if strings.HasSuffix(path, "file") {
				s.False(info.IsDir())
				s.True(info.Mode().IsRegular())
			} else {
				s.True(info.IsDir())
			}
			entries = append(entries, path)
			return nil
		}))
		s.Len(entries, 4)
		for _, item := range []string{
			s.TempPath("test"),
			s.TempPath("test/subdir"),
			s.TempPath("test/subdir/nestedfile"),
			s.TempPath("test/file"),
		} {
			s.Contains(entries, item)
		}
	})
}
