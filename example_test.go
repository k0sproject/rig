package rig_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	rig "github.com/k0sproject/rig/v2"
	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/initsystem"
	rigos "github.com/k0sproject/rig/v2/os"
	"github.com/k0sproject/rig/v2/packagemanager"
	"github.com/k0sproject/rig/v2/remotefs"
	"github.com/k0sproject/rig/v2/rigtest"
)

// ExampleNewClient_localhost demonstrates connecting to the local machine via
// the localhost protocol and running a command.
func ExampleNewClient_localhost() {
	client, err := rig.NewClient(
		rig.WithConnectionFactory(&rig.CompositeConfig{Localhost: true}),
	)
	if err != nil {
		fmt.Println("create client:", err)
		return
	}
	if err := client.Connect(context.Background()); err != nil {
		fmt.Println("connect:", err)
		return
	}
	defer client.Disconnect()

	out, err := client.ExecOutput("echo hello")
	if err != nil {
		fmt.Println("exec:", err)
		return
	}
	fmt.Println(out)
	// Output:
	// hello
}

// ExampleClient_Exec demonstrates running a simple command and checking its
// exit status.
func ExampleClient_Exec() {
	runner := rigtest.NewMockRunner()
	runner.AddCommand(rigtest.Equal("true"), func(_ *rigtest.A) error { return nil })
	runner.AddCommand(rigtest.Equal("false"), func(_ *rigtest.A) error {
		return errors.New("exit status 1")
	})

	if err := runner.Exec("true"); err != nil {
		fmt.Println("unexpected error:", err)
		return
	}
	fmt.Println("true: ok")

	if err := runner.Exec("false"); err != nil {
		fmt.Println("false: failed as expected")
	}
	// Output:
	// true: ok
	// false: failed as expected
}

// ExampleClient_ExecOutput demonstrates capturing stdout from a command.
func ExampleClient_ExecOutput() {
	runner := rigtest.NewMockRunner()
	runner.AddCommandOutput(rigtest.Equal("hostname"), "node-01.example.com\n")

	out, err := runner.ExecOutput("hostname")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(out)
	// Output:
	// node-01.example.com
}

// ExampleClient_Proc demonstrates using cmd.Proc to attach stdin and stdout
// streams before starting a command — similar to configuring os/exec.Cmd.
func ExampleClient_Proc() {
	runner := rigtest.NewMockRunner()
	runner.AddCommand(rigtest.Equal("cat"), func(a *rigtest.A) error {
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(a.Stdin)
		_, _ = fmt.Fprint(a.Stdout, buf.String())
		return nil
	})

	var out strings.Builder
	proc := runner.Proc("cat")
	proc.Stdin = strings.NewReader("hello from proc\n")
	proc.Stdout = &out

	if err := proc.Run(context.Background()); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Print(out.String())
	// Output:
	// hello from proc
}

// ExampleClient_Sudo demonstrates obtaining a sudo-decorated client and using
// it to run a privileged command.
func ExampleClient_Sudo() {
	conn := rigtest.NewMockConnection()
	// Accept the sudo probe ("sudo -n -- ... true").
	conn.AddCommand(rigtest.HasSuffix("true'"), func(_ *rigtest.A) error { return nil })
	// The actual command runs wrapped in sudo.
	conn.AddCommand(rigtest.Contains("id"), func(a *rigtest.A) error {
		fmt.Fprintln(a.Stdout, "uid=0(root)")
		return nil
	})

	client, err := rig.NewClient(rig.WithConnection(conn))
	if err != nil {
		fmt.Println(err)
		return
	}

	out, err := client.Sudo().ExecOutput("id")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(out)
	// Output:
	// uid=0(root)
}

// ExampleClient_FS demonstrates using Client.FS to read a file from the remote
// host via the fs.FS interface.
func ExampleClient_FS() {
	conn := rigtest.NewMockConnection()
	// PosixFS reads file content by running a cat-like command.
	conn.AddCommandOutput(rigtest.Contains("cat"), "node-01\n")

	client, err := rig.NewClient(rig.WithConnection(conn))
	if err != nil {
		fmt.Println(err)
		return
	}

	data, err := client.FS().ReadFile("etc/hostname")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(strings.TrimSpace(string(data)))
	// Output:
	// node-01
}

// ExampleWithOSReleaseProvider demonstrates injecting a custom OS release
// provider that bypasses remote detection.
func ExampleWithOSReleaseProvider() {
	conn := rigtest.NewMockConnection()

	client, err := rig.NewClient(
		rig.WithConnection(conn),
		rig.WithOSReleaseProvider(func(_ cmd.SimpleRunner) (*rigos.Release, error) {
			return &rigos.Release{ID: "alpine", Name: "Alpine Linux", Version: "3.18.0"}, nil
		}),
	)
	if err != nil {
		fmt.Println(err)
		return
	}

	release, err := client.OS()
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("%s %s\n", release.ID, release.Version)
	// Output:
	// alpine 3.18.0
}

// ExampleWithRemoteFSProvider demonstrates injecting a custom filesystem
// provider so that Client.FS returns a specific implementation.
func ExampleWithRemoteFSProvider() {
	conn := rigtest.NewMockConnection()

	client, err := rig.NewClient(
		rig.WithConnection(conn),
		rig.WithRemoteFSProvider(func(_ cmd.Runner) (remotefs.FS, error) {
			return nil, errors.New("filesystem access disabled")
		}),
	)
	if err != nil {
		fmt.Println(err)
		return
	}

	_, err = client.RemoteFSProvider.FS()
	if err != nil {
		fmt.Println("fs:", err)
	}
	// Output:
	// fs: get filesystem: filesystem access disabled
}

// ExampleWithPackageManagerProvider demonstrates injecting a custom package
// manager so that Client.PackageManager returns a known implementation.
func ExampleWithPackageManagerProvider() {
	conn := rigtest.NewMockConnection()

	client, err := rig.NewClient(
		rig.WithConnection(conn),
		rig.WithPackageManagerProvider(func(_ cmd.ContextRunner) (packagemanager.PackageManager, error) {
			return &packagemanager.NullPackageManager{Err: errors.New("no package manager")}, nil
		}),
	)
	if err != nil {
		fmt.Println(err)
		return
	}

	pm := client.PackageManager()
	if err := pm.Install(context.Background(), "curl"); err != nil {
		fmt.Println("install:", err)
	}
	// Output:
	// install: install packages (curl): no package manager
}

// ExampleWithInitSystemProvider demonstrates injecting a custom init system
// provider so that Client.ServiceManager returns a known result.
func ExampleWithInitSystemProvider() {
	conn := rigtest.NewMockConnection()

	client, err := rig.NewClient(
		rig.WithConnection(conn),
		rig.WithInitSystemProvider(func(_ cmd.ContextRunner) (initsystem.ServiceManager, error) {
			return nil, errors.New("no init system detected")
		}),
	)
	if err != nil {
		fmt.Println(err)
		return
	}

	if _, err := client.ServiceManager(); err != nil {
		fmt.Println("service manager:", err)
	}
	// Output:
	// service manager: get service manager: no init system detected
}
