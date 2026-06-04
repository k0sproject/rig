// Package main is a simple file uploader for testing.
package main

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

var errNoConfigurer = errors.New("OS does not support configurer interface")

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
		return fmt.Errorf("getting OS module: %w", err)
	}

	c, ok := bf().(configurer)
	if !ok {
		return fmt.Errorf("%w: %s", errNoConfigurer, *h.OSVersion)
	}
	h.Configurer = c

	return nil
}

func main() {
	destHost := flag.String("host", "127.0.0.1", "target host")
	destPort := flag.Int("port", 9022, "target host port")
	srcFile := flag.String("src", "tmpfile", "source file")
	destFile := flag.String("dst", "/tmp/tempfile", "destination file")
	sudo := flag.Bool("sudo", false, "use sudo when uploading")
	user := flag.String("user", "root", "user name")
	password := flag.String("pass", "", "password")
	proto := flag.String("proto", "ssh", "ssh/winrm")
	https := flag.Bool("https", false, "use https")

	flag.Parse()

	if *destHost == "" {
		println("see -help")
		goos.Exit(1)
	}

	var host *Host

	if *proto == "ssh" {
		host = &Host{
			Connection: rig.Connection{
				SSH: &rig.SSH{
					Address: *destHost,
					Port:    *destPort,
					User:    *user,
				},
			},
		}
	} else {
		host = &Host{
			Connection: rig.Connection{
				WinRM: &rig.WinRM{
					Address:  *destHost,
					Port:     *destPort,
					User:     *user,
					UseHTTPS: *https,
					Insecure: true,
					Password: *password,
				},
			},
		}
	}

	if err := host.Connect(); err != nil {
		fmt.Println(*destHost, *destPort)
		panic(err)
	}

	if err := host.LoadOS(); err != nil {
		panic(err)
	}

	var opts []exec.Option
	if *sudo {
		opts = append(opts, exec.Sudo(host))
	}
	if err := host.Upload(*srcFile, *destFile, 0o600, opts...); err != nil {
		panic(err)
	}
	fmt.Println("Done, file now at", *destFile)
}
