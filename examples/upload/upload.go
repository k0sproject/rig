package main

// A simple file uploader for testing
import (
	"flag"
	"fmt"
	goos "os"

	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/os/registry"
	_ "github.com/k0sproject/rig/os/support"
)

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
		return err
	}

	c, ok := bf().(configurer)
	if !ok {
		return fmt.Errorf("OS %s does not support configurer interface", *h.OSVersion)
	}
	h.Configurer = c

	return nil
}

func main() {
	dh := flag.String("host", "127.0.0.1", "target host")
	dp := flag.Int("port", 9022, "target host port")
	sf := flag.String("src", "tmpfile", "source file")
	df := flag.String("dst", "/tmp/tempfile", "destination file")
	sudo := flag.Bool("sudo", false, "use sudo when uploading")
	usr := flag.String("user", "root", "user name")
	pwd := flag.String("pass", "", "password")
	proto := flag.String("proto", "ssh", "ssh/winrm")
	https := flag.Bool("https", false, "use https")

	flag.Parse()

	if *dh == "" {
		println("see -help")
		goos.Exit(1)
	}

	var h *Host

	if *proto == "ssh" {
		h = &Host{
			Connection: rig.Connection{
				SSH: &rig.SSH{
					Address: *dh,
					Port:    *dp,
					User:    *usr,
				},
			},
		}
	} else {
		h = &Host{
			Connection: rig.Connection{
				WinRM: &rig.WinRM{
					Address:  *dh,
					Port:     *dp,
					User:     *usr,
					UseHTTPS: *https,
					Insecure: true,
					Password: *pwd,
				},
			},
		}
	}

	if err := h.Connect(); err != nil {
		fmt.Println(*dh, *dp)
		panic(err)
	}

	if err := h.LoadOS(); err != nil {
		panic(err)
	}

	var opts []exec.Option
	if *sudo {
		opts = append(opts, exec.Sudo(h))
	}
	if err := h.Upload(*sf, *df, opts...); err != nil {
		panic(err)
	}
	fmt.Println("Done, file now at", *df)
}
