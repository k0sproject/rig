package main

import (
	"bytes"
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
	"github.com/kevinburke/ssh_config"
	"github.com/stretchr/testify/require"
)

type TestingT interface {
	Errorf(format string, args ...interface{})
	FailNow()
}

type testRunner struct{}

func (t testRunner) Run(name string, args ...interface{}) {
	fmt.Println("* Running test:", fmt.Sprintf(name, args...))
}

func (t testRunner) Errorf(format string, args ...interface{}) {
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
	return nil
}

func main() {
	dh := flag.String("host", "127.0.0.1", "target host [+ :port], can give multiple comma separated")
	usr := flag.String("user", "root", "user name")
	proto := flag.String("proto", "ssh", "ssh/winrm")
	kp := flag.String("keypath", "", "ssh keypath")
	pc := flag.Bool("askpass", false, "ask ssh passwords")
	pwd := flag.String("pass", "", "winrm password")
	https := flag.Bool("https", false, "use https for winrm")

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
		case "winrm":
			h = &Host{
				Connection: rig.Connection{
					WinRM: &rig.WinRM{
						Address:  *dh,
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

		t.Run("load os %s", h.Address())
		require.NoError(t, h.LoadOS(), "load os")

		t.Run("os support module functions on %s", h)

		require.NoError(t, h.Configurer.WriteFile(h, fn, "test\ntest2\ntest3", "0644"), "write file")
		if !h.Configurer.FileExist(h, fn) {
			t.Fail("file does not exist after write")
		}
		require.NoError(t, h.Configurer.LineIntoFile(h, fn, "test2", "test4"), "line into file")

		row, err := h.Configurer.ReadFile(h, fn)
		require.NoError(t, err, "read file")
		require.Equal(t, "test\ntest4\ntest3", row, "file content not as expected after line into file")

		stat, err := h.Configurer.Stat(h, fn)
		require.NoError(t, err, "stat error")
		require.Equal(t, filepath.Base(stat.Name()), filepath.Base(fn), "stat name not as expected")

		require.NoError(t, h.Configurer.DeleteFile(h, fn))
		require.False(t, h.Configurer.FileExist(h, fn))

		testFileSize := int64(1 << (10 * 2)) // 1MB
		fsyses := []rig.FS{h.Fsys(), h.SudoFsys()}

		for idx, fsys := range fsyses {
			t.Run("fsys functions (%d) on %s", idx+1, h)

			origin := io.LimitReader(rand.Reader, testFileSize)
			shasum := sha256.New()
			reader := io.TeeReader(origin, shasum)

			destf, err := fsys.OpenFile(fn, rig.ModeCreate, 0644)
			require.NoError(t, err, "open file")

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

			destf, err = fsys.OpenFile(fn, rig.ModeRead, 0644)
			require.NoError(t, err, "open file for read")

			readSha := sha256.New()
			n, err = io.Copy(readSha, destf)
			require.NoError(t, err, "io.copy file from remote to local")

			require.Equal(t, testFileSize, n, "file size not as expected after copy from remote to local")

			fstat, err = destf.Stat()
			require.NoError(t, err, "stat error after read")
			require.Equal(t, testFileSize, fstat.Size(), "file size not as expected in stat result after read")
			require.True(t, bytes.Equal(readSha.Sum(nil), shasum.Sum(nil)), "sha256 mismatch after io.copy from remote to local")

			_, err = destf.Seek(0, 0)
			require.NoError(t, err, "seek")

			readSha.Reset()

			n, err = io.Copy(readSha, destf)
			require.NoError(t, err, "io.copy file from remote to local after seek")

			require.Equal(t, testFileSize, n, "file size not as expected after copy from remote to local after seek")

			require.True(t, bytes.Equal(readSha.Sum(nil), shasum.Sum(nil)), "sha256 mismatch after io.copy from remote to local after seek")

			require.NoError(t, destf.Close(), "close after seek + read")
			require.NoError(t, fsys.Delete(fn), "remove file")
			_, err = destf.Stat()
			require.ErrorIs(t, err, fs.ErrNotExist, "file still exists")

			require.NoError(t, h.Configurer.MkDir(h, "tmp/testdir/subdir"), "make dir")
			require.NoError(t, h.Configurer.WriteFile(h, "tmp/testdir/subdir/testfile1", "test", "0644"), "write file")
			require.NoError(t, h.Configurer.WriteFile(h, "tmp/testdir/testfile2", "test", "0644"), "write file")

			var foundFiles []fs.DirEntry

			err = fs.WalkDir(fsys, "tmp/testdir", func(path string, d fs.DirEntry, err error) error {
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
	}
}
