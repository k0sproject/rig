package main

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	goos "os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/os/registry"
	_ "github.com/k0sproject/rig/os/support"
	"github.com/k0sproject/rig/pkg/rigfs"
	"github.com/kevinburke/ssh_config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type TestingT interface {
	Errorf(format string, args ...any)
	FailNow()
}

type testRunner struct{}

func (t testRunner) Run(name string, args ...any) {
	fmt.Println("* Running test:", fmt.Sprintf(name, args...))
}

func (t testRunner) Errorf(format string, args ...any) {
	println(fmt.Sprintf(format, args...))
}

func (t testRunner) FailNow() {
	panic("fail")
}

func (t testRunner) Fail(msg string) {
	panic("fail: " + msg)
}

func (t testRunner) Err(err error) {
	panic("fail: " + err.Error())
}

type configurer interface {
	WriteFile(os.Host, string, string, string) error
	LineIntoFile(os.Host, string, string, string) error
	ReadFile(os.Host, string) (string, error)
	FileExist(os.Host, string) bool
	DeleteFile(os.Host, string) error
	Stat(os.Host, string, ...exec.Option) (*os.FileInfo, error)
	Touch(os.Host, string, time.Time, ...exec.Option) error
	MkDir(os.Host, string, ...exec.Option) error
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

func main() {
	dh := flag.String("host", "127.0.0.1", "target host [+ :port], can give multiple comma separated")
	usr := flag.String("user", "root", "user name")
	proto := flag.String("proto", "ssh", "ssh/winrm/localhost/openssh")
	kp := flag.String("keypath", "", "ssh keypath")
	pc := flag.Bool("askpass", false, "ask ssh passwords")
	pwd := flag.String("pass", "", "winrm password")
	https := flag.Bool("https", false, "use https for winrm")
	connectOnly := flag.Bool("connect", false, "just connect and quit")
	sshKey := flag.String("ssh-private-key", "", "ssh private key")
	multiplex := flag.Bool("ssh-multiplex", true, "use ssh multiplexing")
	fsysOnly := flag.Bool("fsys", false, "only test rigfs operations")

	fn := fmt.Sprintf("test_%s.txt", time.Now().Format("20060102150405"))

	flag.Parse()

	if *dh == "" {
		println("at least host required, see -help")
		goos.Exit(1)
	}

	if configPath := goos.Getenv("SSH_CONFIG"); configPath != "" {
		f, err := goos.Open(configPath)
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

	var passfunc func() (string, error)
	if *pc {
		passfunc = func() (string, error) {
			var pass string
			fmt.Print("Password: ")
			fmt.Scanln(&pass)
			return pass, nil
		}
	}

	var hosts []*Host

	for _, address := range strings.Split(*dh, ",") {
		port := 22
		if addr, portstr, ok := strings.Cut(address, ":"); ok {
			address = addr
			p, err := strconv.Atoi(portstr)
			if err != nil {
				panic("invalid port " + portstr)
			}
			port = p
		}

		var h *Host
		switch *proto {
		case "ssh":
			if *sshKey != "" {
				// test with private key in a string
				authM, err := rig.ParseSSHPrivateKey([]byte(*sshKey), rig.DefaultPasswordCallback)
				if err != nil {
					panic(err)
				}
				h = &Host{
					Connection: rig.Connection{
						SSH: &rig.SSH{
							Address:     address,
							Port:        port,
							User:        *usr,
							AuthMethods: authM,
						},
					},
				}
			} else {
				h = &Host{
					Connection: rig.Connection{
						SSH: &rig.SSH{
							Address:          address,
							Port:             port,
							User:             *usr,
							KeyPath:          kp,
							PasswordCallback: passfunc,
						},
					},
				}
			}
		case "winrm":
			h = &Host{
				Connection: rig.Connection{
					WinRM: &rig.WinRM{
						Address:  address,
						Port:     port,
						User:     *usr,
						UseHTTPS: *https,
						Insecure: true,
						Password: *pwd,
					},
				},
			}
		case "localhost":
			h = &Host{
				Connection: rig.Connection{
					Localhost: &rig.Localhost{
						Enabled: true,
					},
				},
			}
		case "openssh":
			h = &Host{
				Connection: rig.Connection{
					OpenSSH: &rig.OpenSSH{
						Address:             address,
						KeyPath:             kp,
						DisableMultiplexing: !*multiplex,
					},
				},
			}
			if *usr != "" {
				h.OpenSSH.User = usr
			}
			if port != 22 && port != 0 {
				h.OpenSSH.Port = &port
			}
			if cfgPath := goos.Getenv("SSH_CONFIG"); cfgPath != "" {
				h.OpenSSH.ConfigPath = &cfgPath
			}
		default:
			panic("unknown protocol " + *proto)
		}
		hosts = append(hosts, h)
	}

	t := testRunner{}

	for _, h := range hosts {
		t.Run("connect %s", h.Address())
		err := retry(func() error {
			err := h.Connect()
			if errors.Is(err, rig.ErrCantConnect) {
				t.Err(err)
			}
			return err
		})

		require.NoError(t, err, "connection failed")

		if *connectOnly {
			continue
		}

		if !*fsysOnly {
			t.Run("load os %s", h.Address())
			require.NoError(t, h.LoadOS(), "load os")

			t.Run("os support module functions on %s", h)

			stat, err := h.Configurer.Stat(h, fn)
			require.Error(t, err, "no stat error")

			now := time.Now()
			err = h.Configurer.Touch(h, fn, now)
			require.NoError(t, err, "touch error")

			stat, err = h.Configurer.Stat(h, fn)
			require.NoError(t, err, "stat error")
			assert.Equal(t, filepath.Base(stat.Name()), filepath.Base(fn), "stat name not as expected")
			assert.Equal(t, filepath.Base(stat.Name()), filepath.Base(fn), "stat name not as expected")
			assert.Condition(t, func() bool {
				actual := stat.ModTime()
				return now.Equal(actual) || now.Truncate(time.Second).Equal(actual)
			}, "Expected %s, got %s", now, stat.ModTime())

			require.NoError(t, h.Configurer.WriteFile(h, fn, "test\ntest2\ntest3", "0644"), "write file")
			if !h.Configurer.FileExist(h, fn) {
				t.Fail("file does not exist after write")
			}
			require.NoError(t, h.Configurer.LineIntoFile(h, fn, "test2", "test4"), "line into file")

			row, err := h.Configurer.ReadFile(h, fn)
			require.NoError(t, err, "read file")
			require.Equal(t, "test\ntest4\ntest3", row, "file content not as expected after line into file")

			require.NoError(t, h.Configurer.DeleteFile(h, fn))
			require.False(t, h.Configurer.FileExist(h, fn))
		}

		fsyses := []rigfs.Fsys{h.Fsys()}
		if !h.IsWindows() {
			// on windows using sudo makes no difference - the commands will be executed identically
			// you just might not have permissions to do so. the only access elevation for command line
			// on windows is "runas /user:Administrator" which requires you to enter the password of
			// the Administator account.
			//
			// on linux, we'll test the sudo fsys as well
			fsyses = append(fsyses, h.SudoFsys())
		}

		for idx, fsys := range fsyses {
			for _, testFileSize := range []int64{
				int64(500),           // less than one block on most filesystems
				int64(1 << (10 * 2)), // exactly 1MB
				int64(4096),          // exactly one block on most filesystems
				int64(4097),          // plus 1
			} {
				t.Run("fsys (%d) functions for file size %d on %s", idx+1, testFileSize, h)

				origin := io.LimitReader(rand.Reader, testFileSize)
				shasum := sha256.New()
				reader := io.TeeReader(origin, shasum)

				destf, err := fsys.OpenFile(fn, goos.O_CREATE|goos.O_WRONLY, 0644)
				require.NoError(t, err, "open file using OpenFile")

				n, err := io.Copy(destf, reader)
				require.NoError(t, err, "io.copy file from local to remote")
				require.Equal(t, testFileSize, n, "file size not as expected after copy")

				require.NoError(t, destf.Close(), "error while closing file")

				fstat, err := fsys.Stat(fn)
				require.NoError(t, err, "stat error")
				require.Equal(t, testFileSize, fstat.Size(), "file size not as expected in stat result")

				destSum, err := fsys.Sha256(fn)
				require.NoError(t, err, "sha256 error")

				require.Equal(t, fmt.Sprintf("%x", shasum.Sum(nil)), destSum, "sha256 mismatch after io.copy from local to remote")

				destf, err = fsys.OpenFile(fn, goos.O_RDONLY, 0)
				require.NoError(t, err, "open file for read")

				readSha := sha256.New()
				n, err = io.Copy(readSha, destf)
				require.NoError(t, err, "io.copy file from remote to local")

				require.Equal(t, testFileSize, n, "file size not as expected after copy from remote to local")

				fstat, err = destf.Stat()
				require.NoError(t, err, "stat error after read")
				require.Equal(t, testFileSize, fstat.Size(), "file size not as expected in stat result after read")
				require.Equal(t, readSha.Sum(nil), shasum.Sum(nil), "sha256 mismatch after io.copy from remote to local")

				_, err = destf.Seek(0, 0)
				require.NoError(t, err, "seek")

				readSha.Reset()

				n, err = io.Copy(readSha, destf)
				require.NoError(t, err, "io.copy file from remote to local after seek")

				require.Equal(t, testFileSize, n, "file size not as expected after copy from remote to local after seek")

				require.Equal(t, readSha.Sum(nil), shasum.Sum(nil), "sha256 mismatch after io.copy from remote to local after seek")

				require.NoError(t, destf.Close(), "close after seek + read")
				require.NoError(t, fsys.Remove(fn), "remove file")
				_, err = destf.Stat()
				require.ErrorIs(t, err, fs.ErrNotExist, "file still exists")
			}
			t.Run("fsys (%d) dir ops on %s", idx+1, h)

			// fsys dirops
			require.NoError(t, fsys.MkDirAll("rigtmpdir/nested", 0644), "make nested dir")
			_, err = fsys.Stat("rigtmpdir")
			require.NoError(t, err, "rigtmpdir was not created")
			_, err = fsys.Stat("rigtmpdir/nested")
			require.NoError(t, err, "tmpdir/nested was not created")

			require.NoError(t, fsys.RemoveAll("rigtmpdir"), "remove recursive")
			_, err = fsys.Stat("rigtmpdir/nested")
			require.ErrorIs(t, err, fs.ErrNotExist, "nested dir still exists")
			_, err = fsys.Stat("rigtmpdir")
			require.ErrorIs(t, err, fs.ErrNotExist, "dir still exists")

			// create test dir structure
			require.NoError(t, fsys.MkDirAll("rigtmpdir/testdir/subdir", 0755), "make dir")

			for _, fn := range []string{"rigtmpdir/testdir/subdir/testfile1", "rigtmpdir/testdir/testfile2"} {
				f, err := fsys.OpenFile(fn, goos.O_CREATE|goos.O_WRONLY, 0644)
				require.NoError(t, err, "open file using OpenFile")
				_, err = f.Write([]byte("test"))
				require.NoError(t, err, "write to file")
				require.NoError(t, f.Close(), "close file")
			}

			var foundFiles []fs.DirEntry

			err = fs.WalkDir(fsys, "rigtmpdir/testdir", func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					println("error walking", path, err)
					return err
				}
				info, err := d.Info()
				if err != nil {
					return err
				}
				if info.Mode()&fs.ModeIrregular != 0 {
					return fs.SkipDir
				}

				foundFiles = append(foundFiles, d)
				return nil
			})
			require.NoError(t, err, "walk dir")
			require.Equal(t, 4, len(foundFiles), "walk dir found files")
			require.Equal(t, "testdir", foundFiles[0].Name(), "walk dir found subdir")
			require.Equal(t, "subdir", foundFiles[1].Name(), "walk dir found subdir")
			require.Equal(t, "testfile1", foundFiles[2].Name(), "walk dir found testfile1")
			require.Equal(t, "testfile2", foundFiles[3].Name(), "walk dir found testfile2")
		}
		t.Run("disconnect %s", h.Address())
		h.Disconnect()
	}
}
