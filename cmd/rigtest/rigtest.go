package main

import (
	"flag"
	"fmt"
	goos "os"
	"strings"
	"time"

	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/os/registry"
	_ "github.com/k0sproject/rig/os/support"
	"github.com/kevinburke/ssh_config"
)

type configurer interface {
	WriteFile(os.Host, string, string, string) error
	LineIntoFile(os.Host, string, string, string) error
	ReadFile(os.Host, string) (string, error)
	FileExist(os.Host, string) bool
	DeleteFile(os.Host, string) error
	Stat(os.Host, string, ...exec.Option) (*os.FileInfo, error)
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

func main() {
	dh := flag.String("host", "127.0.0.1", "target host")
	dp := flag.Int("port", 22, "target host port")
	usr := flag.String("user", "root", "user name")
	kp := flag.String("keypath", "", "keypath")

	fn := fmt.Sprintf("test_%s.txt", time.Now().Format("20060102150405"))

	flag.Parse()

	if *dh == "" {
		println("see -help")
		goos.Exit(1)
	}

	h := Host{
		Connection: rig.Connection{
			SSH: &rig.SSH{
				Address: *dh,
				Port:    *dp,
				User:    *usr,
				KeyPath: kp,
			},
		},
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

	if err := h.Connect(); err != nil {
		panic(err)
	}

	if err := h.LoadOS(); err != nil {
		panic(err)
	}

	if err := h.Configurer.WriteFile(h, fn, "test\ntest2\ntest3", "0644"); err != nil {
		panic(err)
	}

	if err := h.Configurer.LineIntoFile(h, fn, "test2", "test4"); err != nil {
		panic(err)
	}

	if !h.Configurer.FileExist(h, fn) {
		panic("file does not exist")
	}

	row, err := h.Configurer.ReadFile(h, fn)
	if err != nil {
		panic(err)
	}
	if row != "test\ntest4\ntest3" {
		panic("file content is not correct")
	}

	stat, err := h.Configurer.Stat(h, fn)
	if err != nil {
		panic(err)
	}
	if !strings.HasSuffix(stat.FName, fn) {
		panic("file stat is not correct")
	}

	if err := h.Configurer.DeleteFile(h, fn); err != nil {
		panic(err)
	}

	if h.Configurer.FileExist(h, fn) {
		panic("file still exists")
	}
}
