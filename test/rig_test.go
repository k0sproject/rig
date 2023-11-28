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
	"path/filepath"
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

func (s *SuiteLogger) Tracef(msg string, args ...interface{}) { s.t.Logf("TRACE "+msg, args...) }
func (s *SuiteLogger) Debugf(msg string, args ...interface{}) { s.t.Logf("DEBUG "+msg, args...) }
func (s *SuiteLogger) Infof(msg string, args ...interface{})  { s.t.Logf("INFO "+msg, args...) }
func (s *SuiteLogger) Warnf(msg string, args ...interface{})  { s.t.Logf("WARN "+msg, args...) }
func (s *SuiteLogger) Errorf(msg string, args ...interface{}) { s.t.Logf("ERROR "+msg, args...) }

type ConnectedSuite struct {
	suite.Suite
	tempDir string
	count   int
	Host    *Host
}

func (s *ConnectedSuite) SetupSuite() {
	rig.SetLogger(&SuiteLogger{s.T()})
	err := retry(func() error { return s.Host.Connect() })
	s.NoError(err)
	s.NoError(s.Host.LoadOS())
	s.tempDir = "tmp.rig-test." + time.Now().Format("20060102150405")
	s.NoError(s.Host.Configurer.MkDir(s.Host, s.tempDir))
}

func (s *ConnectedSuite) TearDownSuite() {
	if s.Host == nil {
		return
	}
	if s.Host.Fsys().RemoveAll(s.tempDir) != nil {
		_ = s.Host.SudoFsys().RemoveAll(s.tempDir)
	}
	s.Host.Disconnect()
}

func (s *ConnectedSuite) TestPath(args ...string) string {
	if len(args) == 0 {
		s.count++
		return filepath.Join(s.tempDir, fmt.Sprintf("testfile.%d", s.count))
	}
	args[0] = filepath.Join(s.tempDir, args[0])
	return filepath.Join(args...)
}

type ConfigurerSuite struct {
	ConnectedSuite
}

func (s *ConfigurerSuite) TestStat() {
	s.Run("File does not exist", func() {
		stat, err := s.Host.Configurer.Stat(s.Host, s.TestPath("doesnotexist"))
		s.Nil(stat)
		s.Error(err)
	})

	s.Run("File exists", func() {
		f := s.TestPath()
		s.Run("Create file", func() {
			s.NoError(s.Host.Configurer.Touch(s.Host, f, time.Now()))
		})

		stat, err := s.Host.Configurer.Stat(s.Host, f)
		s.NoError(err)
		s.Equal(filepath.Base(stat.Name()), filepath.Base(f))
	})
}

func (s *ConfigurerSuite) TestTouch() {
	f := s.TestPath()
	now := time.Now()
	for _, tt := range []time.Time{now, now.Add(1 * time.Hour)} {
		s.Run("Update timestamp "+tt.String(), func() {
			s.NoError(s.Host.Configurer.Touch(s.Host, f, now))
		})

		s.Run("File exists and has correct timestamp "+tt.String(), func() {
			stat, err := s.Host.Configurer.Stat(s.Host, f)
			s.NoError(err)
			s.NotNil(stat)
			s.Equal(now.Unix(), stat.ModTime().Unix())
		})
	}
}

func (s *ConfigurerSuite) TestFileAccess() {
	f := s.TestPath()
	deleted := false
	defer func() {
		if !deleted {
			_ = s.Host.Configurer.DeleteFile(s.Host, f)
		}
	}()
	s.Run("File does not exist", func() {
		s.False(s.Host.Configurer.FileExist(s.Host, f))
	})

	s.Run("Write file", func() {
		s.NoError(s.Host.Configurer.WriteFile(s.Host, f, "test\ntest2\ntest3", "0644"))
	})

	s.Run("File exists", func() {
		s.True(s.Host.Configurer.FileExist(s.Host, f))
	})

	s.Run("Read file and verify contents", func() {
		content, err := s.Host.Configurer.ReadFile(s.Host, f)
		s.NoError(err)
		s.Equal("test\ntest2\ntest3", content)
	})

	s.Run("Replace line in file", func() {
		s.NoError(s.Host.Configurer.LineIntoFile(s.Host, f, "test2", "test4"))
	})

	s.Run("Re-read file and verify contents", func() {
		content, err := s.Host.Configurer.ReadFile(s.Host, f)
		s.NoError(err)
		// TODO: LineIntoFile adds a trailing newline
		s.Equal("test\ntest4\ntest3", strings.TrimSpace(content))
	})

	s.Run("Delete file", func() {
		s.NoError(s.Host.Configurer.DeleteFile(s.Host, f))
		deleted = true
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
			s.NoError(err)
			defer os.Remove(fn)
			defer s.Host.Configurer.DeleteFile(s.Host, s.TestPath(filepath.Base(fn)))

			s.Run("Upload file", func() {
				s.NoError(s.Host.Upload(fn, s.TestPath(filepath.Base(fn))))
			})

			s.Run("Verify file size", func() {
				stat, err := s.Host.Configurer.Stat(s.Host, s.TestPath(filepath.Base(fn)))
				s.NoError(err)
				s.Equal(size, stat.Size())
			})

			s.Run("Verify file contents", func() {
				content, err := s.Host.Configurer.ReadFile(s.Host, s.TestPath(filepath.Base(fn)))
				os.WriteFile("remote-file", []byte(content), 0644)
				s.NoError(err)
				tmpFileContent, err := os.ReadFile(fn)
				os.WriteFile("local-file", tmpFileContent, 0644)
				s.NoError(err)
				s.Equal([]byte(content), tmpFileContent)
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
	testPath := s.TestPath("test", "subdir")
	defer func() {
		_ = s.fsys.RemoveAll(testPath)
	}()
	s.Run("Create directory", func() {
		s.NoError(s.fsys.MkDirAll(testPath, 0755))
	})
	s.Run("Verify directory exists", func() {
		stat, err := s.fsys.Stat(testPath)
		s.NoError(err)
		s.Run("Check permissions", func() {
			if s.Host.IsWindows() {
				s.T().Skip("Windows does not support chmod permissions")
			}
			s.Equal(os.FileMode(0755), stat.Mode().Perm())
			parent, err := s.fsys.Stat(filepath.Dir(testPath))
			s.NoError(err)
			s.Equal(os.FileMode(0755), parent.Mode().Perm())
		})
	})
}

func (s *FsysSuite) TestRemove() {
	testPath := s.TestPath("test", "subdir")
	s.Run("Create directory", func() {
		s.NoError(s.fsys.MkDirAll(testPath, 0755))
	})
	s.Run("Remove directory", func() {
		s.NoError(s.fsys.RemoveAll(testPath))
	})
	s.Run("Verify directory does not exist", func() {
		stat, err := s.fsys.Stat(testPath)
		s.Nil(stat)
		s.Error(err)
		s.True(os.IsNotExist(err))
	})
	s.Run("Remove parent directory", func() {
		s.NoError(s.fsys.RemoveAll(filepath.Dir(testPath)))
	})
	s.Run("Verify parent directory does not exist", func() {
		stat, err := s.fsys.Stat(filepath.Dir(testPath))
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
			fn := s.TestPath()

			origin := io.LimitReader(rand.Reader, testFileSize)
			shasum := sha256.New()
			reader := io.TeeReader(origin, shasum)

			defer func() {
				_ = s.fsys.Remove(fn)
			}()
			s.Run("Write file", func() {
				f, err := s.fsys.OpenFile(fn, os.O_CREATE|os.O_WRONLY, 0644)
				s.Require().NoError(err)
				n, err := io.Copy(f, reader)
				s.Require().NoError(err)
				s.Equal(testFileSize, n)
				s.NoError(f.Close())
			})

			s.Run("Verify file size", func() {
				stat, err := s.fsys.Stat(fn)
				s.Require().NoError(err)
				s.Equal(testFileSize, stat.Size())
			})

			s.Run("Verify file sha256", func() {
				sum, err := s.fsys.Sha256(fn)
				s.NoError(err)
				s.Equal(hex.EncodeToString(shasum.Sum(nil)), sum)
			})

			readSha := sha256.New()
			s.Run("Read file", func() {
				f, err := s.fsys.Open(fn)
				s.Require().NoError(err)
				n, err := io.Copy(readSha, f)
				s.Require().NoError(err)
				s.Equal(testFileSize, n)
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
	fn := s.TestPath()
	reference := bytes.Repeat([]byte{'a'}, 1024)
	defer func() {
		_ = s.fsys.Remove(fn)
	}()
	s.Run("Write file", func() {
		f, err := s.fsys.OpenFile(fn, os.O_CREATE|os.O_WRONLY, 0644)
		s.NoError(err)
		n, err := io.Copy(f, bytes.NewReader(bytes.Repeat([]byte{'a'}, 1024)))
		s.NoError(err)
		s.Equal(int64(1024), n)
		s.NoError(f.Close())
	})
	s.Run("Verify contents", func() {
		f, err := s.fsys.Open(fn)
		s.NoError(err)
		b, err := io.ReadAll(f)
		s.NoError(err)
		s.Equal(1024, len(b))
		s.NoError(f.Close())
		s.Equal(reference, b)
	})
	s.Run("Alter file beginning", func() {
		f, err := s.fsys.OpenFile(fn, os.O_WRONLY, 0644)
		s.NoError(err)
		np, err := f.Seek(0, io.SeekStart)
		s.NoError(err)
		s.Equal(int64(0), np)
		n, err := io.Copy(f, bytes.NewReader(bytes.Repeat([]byte{'b'}, 256)))
		s.NoError(err)
		s.Equal(int64(256), n)
		s.NoError(f.Close())
	})
	copy(reference[0:256], bytes.Repeat([]byte{'b'}, 256))
	s.Run("Verify contents after file beginning altered", func() {
		f, err := s.fsys.Open(fn)
		s.NoError(err)
		b, err := io.ReadAll(f)
		s.NoError(err)
		s.Equal(1024, len(b))
		s.NoError(f.Close())
		s.Equal(reference, b)
	})
	s.Run("Alter file ending", func() {
		f, err := s.fsys.OpenFile(fn, os.O_WRONLY, 0644)
		s.NoError(err)
		np, err := f.Seek(-256, io.SeekEnd)
		s.NoError(err)
		s.Equal(int64(768), np)
		n, err := io.Copy(f, bytes.NewReader(bytes.Repeat([]byte{'c'}, 256)))
		s.NoError(err)
		s.Equal(int64(256), n)
		s.NoError(f.Close())
	})
	copy(reference[768:1024], bytes.Repeat([]byte{'c'}, 256))
	s.Run("Verify contents after file ending altered", func() {
		f, err := s.fsys.Open(fn)
		s.NoError(err)
		b, err := io.ReadAll(f)
		s.NoError(err)
		s.Equal(1024, len(b))
		s.NoError(f.Close())
		s.Equal(reference, b)
	})
	s.Run("Alter file middle", func() {
		f, err := s.fsys.OpenFile(fn, os.O_WRONLY, 0644)
		s.NoError(err)
		np, err := f.Seek(256, io.SeekStart)
		s.NoError(err)
		s.Equal(int64(256), np)
		n, err := io.Copy(f, bytes.NewReader(bytes.Repeat([]byte{'d'}, 512)))
		s.NoError(err)
		s.Equal(int64(512), n)
		s.NoError(f.Close())
	})
	copy(reference[256:768], bytes.Repeat([]byte{'d'}, 512))
	s.Run("Verify contents after file middle altered", func() {
		f, err := s.fsys.Open(fn)
		s.NoError(err)
		b, err := io.ReadAll(f)
		s.NoError(err)
		s.Equal(1024, len(b))
		s.NoError(f.Close())
		s.Equal(reference, b)
	})
}

func (s *FsysSuite) TestReadDir() {
	defer func() {
		_ = s.fsys.RemoveAll(s.TestPath("test"))
	}()
	s.Run("Create directory", func() {
		s.NoError(s.fsys.MkDirAll(s.TestPath("test"), 0755))
	})
	s.Run("Create files", func() {
		for _, fn := range []string{s.TestPath("test", "subdir", "nestedfile"), s.TestPath("test", "file")} {
			s.NoError(s.fsys.MkDirAll(filepath.Dir(fn), 0755))
			f, err := s.fsys.OpenFile(fn, os.O_CREATE|os.O_WRONLY, 0644)
			s.NoError(err)
			n, err := f.Write([]byte("test"))
			s.NoError(err)
			s.Equal(4, n)
			s.NoError(f.Close())
		}
	})

	s.Run("Read directory", func() {
		dir, err := s.fsys.OpenFile(s.TestPath("test"), os.O_RDONLY, 0644)
		s.NoError(err)
		s.Require().NotNil(dir)
		readDirFile, ok := dir.(fs.ReadDirFile)
		s.Require().True(ok)
		entries, err := readDirFile.ReadDir(-1)
		s.Require().NoError(err)
		s.Require().Len(entries, 2)
		s.Equal("file", entries[0].Name())
		s.False(entries[0].IsDir())
		s.Equal("subdir", entries[1].Name())
		s.True(entries[1].IsDir())
	})

	s.Run("Walkdir", func() {
		var entries []string
		s.NoError(fs.WalkDir(s.fsys, s.TestPath("test"), func(path string, d fs.DirEntry, err error) error {
			s.NoError(err)
			info, err := d.Info()
			s.NoError(err)
			if strings.HasSuffix(path, "file") {
				s.False(info.IsDir())
				s.True(info.Mode().IsRegular())
			} else {
				s.True(info.IsDir())
				s.False(info.Mode().IsRegular())
			}
			entries = append(entries, path)
			return nil
		}))
		s.Equal(
			[]string{
				s.TestPath("test"),
				s.TestPath("test/file"),
				s.TestPath("test/subdir"),
				s.TestPath("test/subdir/nestedfile"),
			},
			entries,
		)
	})
}
