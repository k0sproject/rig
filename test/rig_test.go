package test

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/localhost"
	"github.com/k0sproject/rig/log"
	"github.com/k0sproject/rig/openssh"
	"github.com/k0sproject/rig/remotefs"
	"github.com/k0sproject/rig/ssh"
	"github.com/k0sproject/rig/stattime"
	"github.com/k0sproject/rig/winrm"
	"github.com/kevinburke/ssh_config"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// Define variables directly
var (
	targetHost      string
	targetPort      int
	username        string
	protocol        string
	keyPath         string
	configPath      string
	password        string
	useHTTPS        bool
	onlyConnect     bool
	privateKey      string
	enableMultiplex bool
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
	flag.StringVar(&protocol, "protocol", "ssh", "ssh/winrm/localhost/openssh")
	flag.StringVar(&keyPath, "ssh-keypath", "", "ssh keypath")
	flag.StringVar(&configPath, "ssh-configpath", "", "ssh config path")
	flag.StringVar(&privateKey, "ssh-private-key", "", "ssh private key")
	flag.StringVar(&password, "winrm-password", "", "winrm password")
	flag.BoolVar(&useHTTPS, "winrm-https", false, "use https for winrm")
	flag.BoolVar(&enableMultiplex, "openssh-multiplex", true, "use ssh multiplexing")
	flag.BoolVar(&onlyConnect, "connect", false, "only connect to host, dont run other tests")

	// Parse the flags
	flag.Parse()

	if targetHost == "" {
		// no host, nothing to test
		return
	}

	if targetPort == 22 && protocol == "winrm" {
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
		cfg, err := ssh_config.Decode(f)
		if err != nil {
			panic(err)
		}
		ssh.SSHConfigGetAll = func(dst, key string) []string {
			res, err := cfg.GetAll(dst, key)
			if err != nil {
				return nil
			}
			return res
		}
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
	err := retry(func() error { return h.Connect() })
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

func retry(fn func() error) error {
	var err error
	for i := 0; i < 3; i++ {
		err = fn()
		if err == nil {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return err
}

func GetHost(t *testing.T, options ...rig.Option) *Host {
	var client rig.Connection
	switch protocol {
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
		client, _ = localhost.NewConnection(localhost.Config{Enabled: true})
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
	opts := []rig.Option{rig.WithClient(client), rig.WithLoggerFactory(
		func(client rig.Connection) log.Logger {
			return log.NewPrefixLog(&SuiteLogger{t}, client.String()+": ")
		}),
	}
	opts = append(opts, options...)
	conn, err := rig.NewConnection(opts...)
	require.NoError(t, err)
	return &Host{Client: conn}
}

type SuiteLogger struct {
	t *testing.T
}

func (s *SuiteLogger) Tracef(msg string, args ...interface{}) {
	s.t.Helper()
	s.t.Logf("%s TRACE %s", time.Now(), fmt.Sprintf(msg, args...))
}
func (s *SuiteLogger) Debugf(msg string, args ...interface{}) {
	s.t.Helper()
	s.t.Logf("%s DEBUG %s", time.Now(), fmt.Sprintf(msg, args...))
}
func (s *SuiteLogger) Infof(msg string, args ...interface{}) {
	s.t.Helper()
	s.t.Logf("%s INFO %s", time.Now(), fmt.Sprintf(msg, args...))
}
func (s *SuiteLogger) Warnf(msg string, args ...interface{}) {
	s.t.Helper()
	s.t.Logf("%s WARN %s", time.Now(), fmt.Sprintf(msg, args...))
}
func (s *SuiteLogger) Errorf(msg string, args ...interface{}) {
	s.t.Helper()
	s.t.Logf("%s ERROR %s", time.Now(), fmt.Sprintf(msg, args...))
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
	err := retry(func() error { return s.Host.Connect() })
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
	for _, tt := range []time.Time{now, now.Add(1 * time.Hour)} {
		s.Run("Update timestamp "+tt.String(), func() {
			s.Require().NoError(s.fs.Chtimes(f, now.UnixNano(), now.UnixNano()))
		})

		s.Run("File exists and has correct timestamp "+tt.String(), func() {
			stat, err := s.fs.Stat(f)
			s.Require().NoError(err)
			s.NotNil(stat)
			s.Equal(now.Unix(), stat.ModTime().Unix())
			if s.Host.IsWindows() {
				s.T().Log("Testing millisecond precision on windows")
				s.Equal(now.UnixMilli(), stat.ModTime().UnixMilli(), "expected %d (%s), got %d (%s)", now.UnixMilli(), now, stat.ModTime().UnixMilli(), stat.ModTime())
			} else if stat.ModTime().Nanosecond() != 0 {
				s.T().Log("Testing nanosecond precision")
				s.Equal(now.UnixNano(), stat.ModTime().UnixNano())
			}
			s.True(stattime.Equal(now, stat.ModTime()))
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
	s.T().Log("testmkdir")
	testPath := s.TempPath("test", "subdir")
	defer func() {
		_ = s.fs.RemoveAll(testPath)
	}()
	s.Run("Create directory", func() {
		s.T().Log("mkdirall")
		s.Require().NoError(s.fs.MkdirAll(testPath, 0755))
	})
	s.Run("Verify directory exists", func() {
		s.T().Log("stat")
		stat, err := s.fs.Stat(testPath)
		s.Require().NoError(err)
		s.Run("Check permissions", func() {
			if s.Host.IsWindows() {
				s.T().Skip("Windows does not support chmod permissions")
			}
			s.Equal(os.FileMode(0755), stat.Mode().Perm())
			parent, err := s.fs.Stat(s.TempPath("test"))
			s.Require().NoError(err)
			s.Equal(os.FileMode(0755), parent.Mode().Perm())
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
