package main

// A simple file uploader for testing
import (
	"flag"
	"fmt"
	goos "os"

	"github.com/alessio/shellescape"
	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/os/registry"
	_ "github.com/k0sproject/rig/os/support"
	ps "github.com/k0sproject/rig/powershell"
)

type configurer interface {
	Pwd(os.Host) string
	CheckPrivilege(os.Host) error
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
	dp := flag.Int("port", 9022, "target host port")
	sf := flag.String("src", "tmpfile", "source file")
	df := flag.String("dst", "/tmp/tempfile", "destination file")
	sudo := flag.Bool("sudo", false, "use sudo when uploading")
	usr := flag.String("user", "root", "user name")
	pwd := flag.String("pass", "", "password")
	proto := flag.String("proto", "ssh", "ssh/winrm")

	flag.Parse()

	if *dh == "" {
		println("see -help")
		goos.Exit(1)
	}

	var h Host

	if *proto == "ssh" {
		h = Host{
			Connection: rig.Connection{
				SSH: &rig.SSH{
					Address: *dh,
					Port:    *dp,
					User:    *usr,
				},
			},
		}
	} else {
		h = Host{
			Connection: rig.Connection{
				WinRM: &rig.WinRM{
					Address:  *dh,
					Port:     *dp,
					User:     *usr,
					UseHTTPS: true,
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

	err := h.Upload(*sf, *df, opts...)
	if err != nil {
		panic(err)
	}

	fmt.Println("Done, file now at", *df)
	var md5 string
	if h.IsWindows() {
		md5, err = h.ExecOutputf("certutil -hashfile %s MD5", ps.DoubleQuote(*df))
		if err != nil {
			panic(err)
		}
	} else {
		md5, err = h.ExecOutputf("md5sum %s", shellescape.Quote(*df))
		if err != nil {
			panic(err)
		}
	}
	fmt.Println("md5sum:", md5)
	if err := h.Configurer.CheckPrivilege(h); err != nil {
		panic(err)
	}
}
