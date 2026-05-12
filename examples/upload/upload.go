// Package main demonstrates how to upload files with rig.
package main

// A simple file uploader for testing
import (
	"errors"
	"flag"
	"fmt"
	goos "os"

	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/os/registry"
	_ "github.com/k0sproject/rig/os/support"
)

var errUnsupportedOS = errors.New("OS does not support configurer interface")

type configurer interface {
	Pwd(host os.Host) string
	CheckPrivilege(host os.Host) error
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
		return fmt.Errorf("load os module builder: %w", err)
	}

	c, ok := bf().(configurer)
	if !ok {
		return fmt.Errorf("%w: %s", errUnsupportedOS, *h.OSVersion)
	}
	h.Configurer = c

	return nil
}

func main() {
	hostAddr := flag.String("host", "127.0.0.1", "target host")
	hostPort := flag.Int("port", 9022, "target host port")
	srcFile := flag.String("src", "tmpfile", "source file")
	dstFile := flag.String("dst", "/tmp/tempfile", "destination file")
	sudo := flag.Bool("sudo", false, "use sudo when uploading")
	user := flag.String("user", "root", "user name")
	password := flag.String("pass", "", "password")
	proto := flag.String("proto", "ssh", "ssh/winrm")
	https := flag.Bool("https", false, "use https")

	flag.Parse()

	if *hostAddr == "" {
		println("see -help")
		goos.Exit(1)
	}

	var host *Host

	if *proto == "ssh" {
		host = &Host{
			Connection: rig.Connection{
				SSH: &rig.SSH{
					Address: *hostAddr,
					Port:    *hostPort,
					User:    *user,
				},
			},
		}
	} else {
		host = &Host{
			Connection: rig.Connection{
				WinRM: &rig.WinRM{
					Address:  *hostAddr,
					Port:     *hostPort,
					User:     *user,
					UseHTTPS: *https,
					Insecure: true,
					Password: *password,
				},
			},
		}
	}

	if err := host.Connect(); err != nil {
		fmt.Println(*hostAddr, *hostPort)
		panic(err)
	}

	if err := host.LoadOS(); err != nil {
		panic(err)
	}

	var opts []exec.Option
	if *sudo {
		opts = append(opts, exec.Sudo(host))
	}
	if err := host.Upload(*srcFile, *dstFile, 0o600, opts...); err != nil {
		panic(err)
	}
	fmt.Println("Done, file now at", *dstFile)
}
