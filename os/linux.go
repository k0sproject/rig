package os

import (
	"io"

	"github.com/k0sproject/rig/cmd"
	"github.com/k0sproject/rig/kv"
)

// ResolveLinux resolves the OS release information for a linux host.
func ResolveLinux(conn cmd.SimpleRunner) (*Release, bool) {
	if conn.IsWindows() {
		return nil, false
	}

	if err := conn.Exec("uname | grep -q Linux"); err != nil {
		return nil, false
	}

	pr, pw := io.Pipe()
	defer pw.Close()

	decoder := kv.NewDecoder(pr)

	cmd, err := conn.StartBackground("cat /etc/os-release || cat /usr/lib/os-release", cmd.Stdout(pw))
	if err != nil {
		return nil, false
	}

	var decodeErr error
	version := &Release{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		decodeErr = decoder.Decode(version)
	}()
	<-done
	if err := cmd.Wait(); err != nil {
		return nil, false
	}
	if decodeErr != nil {
		return nil, false
	}

	return version, true
}

// RegisterLinux registers the linux OS release resolver to a provider.
func RegisterLinux(provider *Provider) {
	provider.Register(ResolveLinux)
}
