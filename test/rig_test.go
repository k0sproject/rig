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
	"github.com/k0sproject/rig/exec"
	rigos "github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/os/registry"
	_ "github.com/k0sproject/rig/os/support"
	"github.com/k0sproject/rig/pkg/rigfs"
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
		rig.SSHConfigGetAll = func(dst, key string) []string {
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

	h := GetHost()
	err := retry(func() error { return h.Connect() })
	require.NoError(t, err)
	h.Disconnect()
}

func TestConfigurerSuite(t *testing.T) {
	if onlyConnect {
		t.Skip("only connect")
		return
	}
	suite.Run(t, &ConfigurerSuite{ConnectedSuite: ConnectedSuite{Host: GetHost()}})
}

func TestFsysSuite(t *testing.T) {
	if onlyConnect {
		t.Skip("only connect")
		return
	}

	h := GetHost()
	t.Run("No sudo", func(t *testing.T) {
		suite.Run(t, &FsysSuite{ConnectedSuite: ConnectedSuite{Host: h}})
	})

	t.Run("Sudo", func(t *testing.T) {
		suite.Run(t, &FsysSuite{ConnectedSuite: ConnectedSuite{Host: h}, sudo: true})
	})
}

type configurer interface {
	WriteFile(rigos.Host, string, string, string) error
	LineIntoFile(rigos.Host, string, string, string) error
	ReadFile(rigos.Host, string) (string, error)
	FileExist(rigos.Host, string) bool
	DeleteFile(rigos.Host, string) error
	Stat(rigos.Host, string, ...exec.Option) (*rigos.FileInfo, error)
	Touch(rigos.Host, string, time.Time, ...exec.Option) error
	MkDir(rigos.Host, string, ...exec.Option) error
	Sha256sum(rigos.Host, string, ...exec.Option) (string, error)
	CommandExist(rigos.Host, string) bool
}

// Host is a host that utilizes rig for connections
type Host struct {
	rig.Connection

	Configurer configurer
}

// LoadOS is a function that assigns a OS support package to the host and
// typecasts it to a suitable interface
func (h *Host) LoadOS() error {
	bf, err := registry.GetOSModuleBuilder(*h.OSVersion)
	if err != nil {
		return err
	}

	h.Configurer = bf().(configurer)

	return nil
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

func GetHost() *Host {
	h := &Host{}
	switch protocol {
	case "ssh":
		h.SSH = &rig.SSH{
			Address: targetHost,
			Port:    targetPort,
			User:    username,
		}

		if privateKey != "" {
			authM, err := rig.ParseSSHPrivateKey([]byte(privateKey), rig.DefaultPasswordCallback)
			if err != nil {
				panic(err)
			}
			h.SSH.AuthMethods = authM
		}

		if keyPath != "" {
			h.SSH.KeyPath = &keyPath
		}
	case "winrm":
		h.WinRM = &rig.WinRM{
			Address:  targetHost,
			Port:     targetPort,
			User:     username,
			UseHTTPS: useHTTPS,
			Insecure: true,
			Password: password,
		}
	case "localhost":
		h.Localhost = &rig.Localhost{Enabled: true}
	case "openssh":
		h.OpenSSH = &rig.OpenSSH{
			Address:             targetHost,
			DisableMultiplexing: !enableMultiplex,
		}
		if targetPort != 22 {
			h.OpenSSH.Port = &targetPort
		}

		if keyPath != "" {
			h.OpenSSH.KeyPath = &keyPath
		}
		if username != "" {
			h.OpenSSH.User = &username
		}
		if configPath != "" {
			h.OpenSSH.ConfigPath = &configPath
		}
	default:
		panic("unknown protocol")
	}
	return h
}

type SuiteLogger struct {
	t *testing.T
}

func (s *SuiteLogger) Tracef(msg string, args ...interface{}) {
	s.t.Log(fmt.Sprintf("%s TRACE %s", time.Now(), fmt.Sprintf(msg, args...)))
}

func (s *SuiteLogger) Debugf(msg string, args ...interface{}) {
	s.t.Log(fmt.Sprintf("%s DEBUG %s", time.Now(), fmt.Sprintf(msg, args...)))
}

func (s *SuiteLogger) Infof(msg string, args ...interface{}) {
	s.t.Log(fmt.Sprintf("%s INFO %s", time.Now(), fmt.Sprintf(msg, args...)))
}

func (s *SuiteLogger) Warnf(msg string, args ...interface{}) {
	s.t.Log(fmt.Sprintf("%s WARN %s", time.Now(), fmt.Sprintf(msg, args...)))
}

func (s *SuiteLogger) Errorf(msg string, args ...interface{}) {
	s.t.Log(fmt.Sprintf("%s ERROR %s", time.Now(), fmt.Sprintf(msg, args...)))
}

type ConnectedSuite struct {
	suite.Suite
	tempDir string
	count   int
	Host    *Host
}

func (s *ConnectedSuite) SetupSuite() {
	rig.SetLogger(&SuiteLogger{s.T()})
	err := retry(func() error { return s.Host.Connect() })
	s.Require().NoError(err)
	s.Require().NoError(s.Host.LoadOS())
	s.tempDir = "tmp.rig-test." + time.Now().Format("20060102150405")
	s.Require().NoError(s.Host.Fsys().MkDirAll(s.tempDir, 0o755))
}

func (s *ConnectedSuite) TearDownSuite() {
	if s.Host == nil {
		return
	}
	_ = s.Host.Fsys().RemoveAll(s.tempDir)
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

type ConfigurerSuite struct {
	ConnectedSuite
}

func (s *ConfigurerSuite) TestCommandExist() {
	s.Run("Command exists", func() {
		s.True(s.Host.Configurer.CommandExist(s.Host, "ls"))
	})

	s.Run("Command does not exist", func() {
		s.False(s.Host.Configurer.CommandExist(s.Host, "doesnotexist"))
	})
}

func (s *ConfigurerSuite) TestStat() {
	s.Run("File does not exist", func() {
		stat, err := s.Host.Configurer.Stat(s.Host, s.TempPath("doesnotexist"))
		s.Nil(stat)
		s.Error(err)
	})

	s.Run("File exists", func() {
		f := s.TempPath()
		s.Run("Create file", func() {
			s.Require().NoError(s.Host.Configurer.Touch(s.Host, f, time.Now()))
		})

		stat, err := s.Host.Configurer.Stat(s.Host, f)
		s.Require().NoError(err)
		s.True(strings.HasSuffix(f, stat.Name())) // Name() returns Basename
	})
}

func (s *ConfigurerSuite) TestTouch() {
	f := s.TempPath()
	now := time.Now()
	for _, tt := range []time.Time{now, now.Add(1 * time.Hour)} {
		s.Run("Update timestamp "+tt.String(), func() {
			s.Require().NoError(s.Host.Configurer.Touch(s.Host, f, now))
		})

		s.Run("File exists and has correct timestamp "+tt.String(), func() {
			stat, err := s.Host.Configurer.Stat(s.Host, f)
			s.Require().NoError(err)
			s.NotNil(stat)
			s.Equal(now.Unix(), stat.ModTime().Unix())
		})
	}
}

func (s *ConfigurerSuite) TestFileAccess() {
	f := s.TempPath()
	s.Run("File does not exist", func() {
		s.False(s.Host.Configurer.FileExist(s.Host, f))
	})

	s.Run("Write file", func() {
		s.Require().NoError(s.Host.Configurer.WriteFile(s.Host, f, "test\ntest2\ntest3", "0644"))
	})

	s.Run("File exists", func() {
		s.True(s.Host.Configurer.FileExist(s.Host, f))
	})

	s.Run("Read file and verify contents", func() {
		content, err := s.Host.Configurer.ReadFile(s.Host, f)
		s.Require().NoError(err)
		s.Equal("test\ntest2\ntest3", content)
	})

	s.Run("Replace line in file", func() {
		s.Require().NoError(s.Host.Configurer.LineIntoFile(s.Host, f, "test2", "test4"))
	})

	s.Run("Re-read file and verify contents", func() {
		content, err := s.Host.Configurer.ReadFile(s.Host, f)
		s.Require().NoError(err)
		// TODO: LineIntoFile adds a trailing newline
		s.Equal("test\ntest4\ntest3", strings.TrimSpace(content))
	})

	s.Run("Delete file", func() {
		s.Require().NoError(s.Host.Configurer.DeleteFile(s.Host, f))
	})

	s.Run("File does not exist", func() {
		s.False(s.Host.Configurer.FileExist(s.Host, f))
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

func (s *ConfigurerSuite) TestUpload() {
	for _, size := range []int64{500, 100 * 1024, 1024 * 1024} {
		s.Run(fmt.Sprintf("File size %d", size), func() {
			fn, err := testFile(size)
			s.Require().NoError(err)
			defer os.Remove(fn)
			defer s.Host.Configurer.DeleteFile(s.Host, s.TempPath(pathBase(fn)))

			s.Run("Upload file", func() {
				s.Require().NoError(s.Host.Upload(fn, s.TempPath(pathBase(fn)), 0o600))
			})

			s.Run("Verify file size", func() {
				stat, err := s.Host.Configurer.Stat(s.Host, s.TempPath(pathBase(fn)))
				s.Require().NoError(err)
				s.Require().NotNil(stat)
				s.Equal(size, stat.Size())
			})

			s.Run("Verify file contents", func() {
				sum, err := s.Host.Configurer.Sha256sum(s.Host, s.TempPath(pathBase(fn)))
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

type FsysSuite struct {
	ConnectedSuite
	sudo bool
	fsys rigfs.Fsys
}

func (s *FsysSuite) SetupSuite() {
	s.ConnectedSuite.SetupSuite()
	if s.sudo {
		if s.Host.IsWindows() {
			s.T().Skip("sudo not supported on windows")
			return
		}
		s.fsys = s.Host.SudoFsys()
	} else {
		s.fsys = s.Host.Fsys()
	}
}

func (s *FsysSuite) TestMkdir() {
	s.T().Log("testmkdir")
	testPath := s.TempPath("test", "subdir")
	defer func() {
		_ = s.fsys.RemoveAll(testPath)
	}()
	s.Run("Create directory", func() {
		s.T().Log("mkdirall")
		s.Require().NoError(s.fsys.MkDirAll(testPath, 0o755))
	})
	s.Run("Verify directory exists", func() {
		s.T().Log("stat")
		stat, err := s.fsys.Stat(testPath)
		s.Require().NoError(err)
		s.Run("Check permissions", func() {
			if s.Host.IsWindows() {
				s.T().Skip("Windows does not support chmod permissions")
			}
			s.Equal(os.FileMode(0o755), stat.Mode().Perm())
			parent, err := s.fsys.Stat(s.TempPath("test"))
			s.Require().NoError(err)
			s.Equal(os.FileMode(0o755), parent.Mode().Perm())
		})
	})
}

func (s *FsysSuite) TestRemove() {
	testPath := s.TempPath("test", "subdir")
	s.Run("Create directory", func() {
		s.Require().NoError(s.fsys.MkDirAll(testPath, 0o755))
	})
	s.Run("Remove directory", func() {
		s.Require().NoError(s.fsys.RemoveAll(testPath))
	})
	s.Run("Verify directory does not exist", func() {
		stat, err := s.fsys.Stat(testPath)
		s.Nil(stat)
		s.Error(err)
		s.True(os.IsNotExist(err))
	})
	s.Run("Remove parent directory", func() {
		s.Require().NoError(s.fsys.RemoveAll(s.TempPath("test")))
	})
	s.Run("Verify parent directory does not exist", func() {
		stat, err := s.fsys.Stat(s.TempPath("test"))
		s.Nil(stat)
		s.Error(err)
		s.True(os.IsNotExist(err))
	})
}

func (s *FsysSuite) TestReadWriteFile() {
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
				_ = s.fsys.Remove(fn)
			}()
			s.Run("Write file", func() {
				f, err := s.fsys.OpenFile(fn, os.O_CREATE|os.O_WRONLY, 0o644)
				s.Require().NoError(err)
				n, err := io.Copy(f, reader)
				s.Require().NoError(err)
				s.Equal(testFileSize, n)
				s.Require().NoError(f.Close())
			})

			s.Run("Verify file size", func() {
				stat, err := s.fsys.Stat(fn)
				s.Require().NoError(err)
				s.Equal(testFileSize, stat.Size())
			})

			s.Run("Verify file sha256", func() {
				sum, err := s.fsys.Sha256(fn)
				s.Require().NoError(err)
				s.Equal(hex.EncodeToString(shasum.Sum(nil)), sum)
			})

			readSha := sha256.New()
			s.Run("Read file", func() {
				f, err := s.fsys.Open(fn)
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

func (s *FsysSuite) TestReadWriteFileCopy() {
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
				_ = s.fsys.Remove(fn)
			}()
			s.Run("Write file", func() {
				f, err := s.fsys.OpenFile(fn, os.O_CREATE|os.O_WRONLY, 0o644)
				s.Require().NoError(err)
				n, err := f.CopyFrom(reader)
				s.Require().NoError(err)
				s.Equal(testFileSize, n)
				s.Require().NoError(f.Close())
			})

			s.Run("Verify file size", func() {
				stat, err := s.fsys.Stat(fn)
				s.Require().NoError(err)
				s.Equal(testFileSize, stat.Size())
			})

			s.Run("Verify file sha256", func() {
				sum, err := s.fsys.Sha256(fn)
				s.Require().NoError(err)
				s.Equal(hex.EncodeToString(shasum.Sum(nil)), sum)
			})

			readSha := sha256.New()
			s.Run("Read file", func() {
				fsf, err := s.fsys.Open(fn)
				s.Require().NoError(err)
				f, ok := fsf.(rigfs.File)
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

func (s *FsysSuite) TestSeek() {
	fn := s.TempPath()
	reference := bytes.Repeat([]byte{'a'}, 1024)
	defer func() {
		_ = s.fsys.Remove(fn)
	}()
	f, err := s.fsys.OpenFile(fn, os.O_CREATE|os.O_WRONLY, 0o644)
	s.Require().NoError(err)
	n, err := io.Copy(f, bytes.NewReader(bytes.Repeat([]byte{'a'}, 1024)))
	s.Require().NoError(err)
	s.Equal(int64(1024), n)
	s.Require().NoError(f.Close())

	s.Run("Verify contents", func() {
		f, err := s.fsys.Open(fn)
		s.Require().NoError(err)
		b, err := io.ReadAll(f)
		s.Require().NoError(err)
		s.Equal(1024, len(b))
		s.Require().NoError(f.Close())
		s.Equal(reference, b)
	})
	s.Run("Alter file beginning", func() {
		f, err := s.fsys.OpenFile(fn, os.O_WRONLY, 0o644)
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
		f, err := s.fsys.Open(fn)
		s.Require().NoError(err)
		b, err := io.ReadAll(f)
		s.Require().NoError(err)
		s.Equal(1024, len(b))
		s.Require().NoError(f.Close())
		s.Equal(reference, b)
	})
	s.Run("Alter file ending", func() {
		f, err := s.fsys.OpenFile(fn, os.O_WRONLY, 0o644)
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
		f, err := s.fsys.Open(fn)
		s.Require().NoError(err)
		b, err := io.ReadAll(f)
		s.Require().NoError(err)
		s.Equal(1024, len(b))
		s.Require().NoError(f.Close())
		s.Equal(reference, b)
	})
	s.Run("Alter file middle", func() {
		f, err := s.fsys.OpenFile(fn, os.O_WRONLY, 0o644)
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
		f, err := s.fsys.Open(fn)
		s.Require().NoError(err)
		b, err := io.ReadAll(f)
		s.Require().NoError(err)
		s.Equal(1024, len(b))
		s.Require().NoError(f.Close())
		s.Equal(reference, b)
	})
}

func (s *FsysSuite) TestReadDir() {
	defer func() {
		_ = s.fsys.RemoveAll(s.TempPath("test"))
	}()
	s.Run("Create directory", func() {
		s.Require().NoError(s.fsys.MkDirAll(s.TempPath("test"), 0o755))
	})
	s.Run("Create files", func() {
		for _, fn := range []string{s.TempPath("test", "subdir", "nestedfile"), s.TempPath("test", "file")} {
			s.Require().NoError(s.fsys.MkDirAll(pathDir(fn), 0o755))
			f, err := s.fsys.OpenFile(fn, os.O_CREATE|os.O_WRONLY, 0o644)
			s.Require().NoError(err)
			n, err := f.Write([]byte("test"))
			s.Require().NoError(err)
			s.Equal(4, n)
			s.Require().NoError(f.Close())
		}
	})

	s.Run("Read directory", func() {
		dir, err := s.fsys.OpenFile(s.TempPath("test"), os.O_RDONLY, 0o644)
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
		s.Require().NoError(fs.WalkDir(s.fsys, s.TempPath("test"), func(path string, d fs.DirEntry, err error) error {
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
