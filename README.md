# Rig

[![GoDoc](https://godoc.org/github.com/k0sproject/rig/v2/?status.svg)](https://godoc.org/github.com/k0sproject/rig/v2)
[![Go Report Card](https://goreportcard.com/badge/github.com/k0sproject/rig)](https://goreportcard.com/report/github.com/k0sproject/rig)
[![codecov](https://codecov.io/gh/k0sproject/rig/branch/main/graph/badge.svg)](https://codecov.io/gh/k0sproject/rig)
[![FOSSA Status](https://app.fossa.com/api/projects/git%2Bgithub.com%2Fk0sproject%2Frig.svg?type=shield)](https://app.fossa.com/projects/git%2Bgithub.com%2Fk0sproject%2Frig?ref=badge_shield)

**Rig is a Go library for managing remote hosts over SSH, WinRM or the local
machine with one consistent, testable API.**

Connect to a host once and you get command execution, a remote filesystem that
implements the standard library's `io/fs`, automatic OS/distro detection, sudo
escalation, service control, and package management the same way whether the
target is Linux over SSH, Windows over WinRM or the machine you're running on.

<br clear="left"/>

> **Version note.** This is **rig v2** (`github.com/k0sproject/rig/v2`), a ground-up
> redesign. The stable v0.x line with years of production use history lives on the [`release-0.x`](https://github.com/k0sproject/rig/tree/release-0.x)
> branch.
>
> **Migrating from v0.x?** See **[docs/MIGRATING-from-v0.x.md](docs/MIGRATING-from-v0.x.md)**.

## Install

```sh
go get github.com/k0sproject/rig/v2
```

## Quickstart

```go
package main

import (
	"context"
	"fmt"

	rig "github.com/k0sproject/rig/v2"
	"github.com/k0sproject/rig/v2/protocol/ssh"
)

func main() {
	client, err := rig.NewClient(rig.WithConnection(&ssh.Connection{
		Config: ssh.Config{Address: "10.0.0.5"},
	}))
	if err != nil {
		panic(err)
	}

	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		panic(err)
	}
	defer client.Disconnect()

	out, err := client.ExecOutput("uname -a")
	if err != nil {
		panic(err)
	}
	fmt.Println(out)
}
```

## Features

### One API for any transport

The same `*rig.Client` drives the internal `golang.org/x/crypto/ssh` based client, the system `ssh` binary (OpenSSH, with connection
multiplexing), WinRM, or the pseudo-connection to local machine. Pick a protocol by handing `NewClient` a
connection or for config-driven apps or embed `CompositeConfig` / `ClientWithConfig` to your `host` struct:

```go
client, err := rig.NewClient(rig.WithConnectionFactory(&rig.CompositeConfig{
	SSH: &ssh.Config{Address: "10.0.0.5", User: "root"},
	// or WinRM: &winrm.Config{...}, OpenSSH: &openssh.Config{...}, Localhost: true
}))
```

```go
type Host struct {
  rig.CompositeConfig `yaml:",inline"`
}
```

```yaml
# the same struct, populated from YAML
hosts:
  - ssh:
    address: 10.0.0.5
    user: root
    port: 22
    keyPath: ~/.ssh/id_ed25519
```

SSH connections honour your `~/.ssh/config`'s host aliases, `IdentityFile`,
`UserKnownHostsFile` and a number of other ssh config options. Host-key handling follows OpenSSH conventions and respects and updates `~/.ssh/known_hosts`.

### The remote filesystem is an `fs.FS`

`client.FS()` returns a filesystem that satisfies the standard library `io/fs`
interfaces — so the **entire `io/fs` ecosystem works against a remote host**, no
adapters required:

```go
fsys := client.FS()

data, err := fs.ReadFile(fsys, "/etc/os-release")        // stdlib io/fs
matches, err := fs.Glob(fsys, "/etc/*.conf")             // stdlib io/fs
err = fs.WalkDir(fsys, "/var/log", func(p string, d fs.DirEntry, err error) error {
	fmt.Println(p)                                       // walking a tree over SSH
	return nil
})
```

It also exposes a richer, `os`-like surface — `WriteFile`, `MkdirAll`, `Rename`,
`Chmod`, `Stat`, `OpenFile` (honouring `os.O_EXCL` & friends), plus OS-aware
`NativePath`/`ShellQuote` so the same code is correct on Windows and Linux targets. The function signatures mimic those found in stdlib. Atomic uploads with checksum verification come built in:

```go
err = remotefs.Upload(client.FS(), "local/binary", "/usr/local/bin/tool",
	remotefs.WithPermissions(0o755))
```

### Sudo is just another client

`client.Sudo()` returns a clone of the regular runner whose every command is privilege-escalated (sudo, doas or Windows runas, detected automatically):

```go
sudo := client.Sudo()
sudo.Exec("systemctl restart k0s")            // runs as root
err := sudo.FS().WriteFile("/etc/motd", []byte("hello\n"), 0o644)
if err := client.CheckSudo(ctx); err != nil { /* escalation unavailable */ }
```

### Manage the host

OS detection, service control, and package management are lazy-initialized providers. 
They figure out systemd vs OpenRC, apt vs apk vs chocolatey, and so on:

```go
release, _ := client.OS()                              // *os.Release (id, version, ...)
arch, _ := release.Arch()                              // normalized to GOARCH ("amd64", "arm64", ...)

svc, _ := client.Sudo().Service("k0scontroller")
svc.Enable(ctx); svc.Start(ctx)                        // systemd/openrc/winsvc, abstracted

pm := client.Sudo().PackageManager()
pm.Install(ctx, "curl", "tar")                         // apt/yum/dnf/apk/choco
```

### Stream like `os/exec`

`client.Proc()` gives you an `os/exec.Cmd`-style handle. Wire up stdin/stdout/stderr,
then run:

```go
proc := client.Proc("tar -xzf - -C /opt")
proc.Stdin = archiveReader
proc.Stdout = os.Stdout
err := proc.Run(ctx)
```

Every `Exec`/`ExecOutput` has an
`ExecContext`/`ExecOutputContext` twin that takes a `context.Context` which you can cancel and the remote command will get aborted.

### Testing

`rigtest` provides mock runners and connections so you can unit-test host logic:

```go
runner := rigtest.NewMockRunner()
runner.AddCommandOutput(rigtest.Equal("hostname"), "node-01\n")

out, _ := runner.ExecOutput("hostname")   // "node-01\n" — no host required
```

Program command responses, assert on what ran, and capture logs, see
**[docs/TESTING.md](docs/TESTING.md)**.

### Extensible by injection

Every provider (filesystem, OS release, package manager, init system, logger) can be swapped at construction with a `With*` option, so you can pin behaviour, stub a
subsystem, or add support for something rig doesn't ship (until you make a PR):

```go
client, _ := rig.NewClient(
	rig.WithConnection(conn),
	rig.WithLogger(slog.New(myHandler)),
	rig.WithOSReleaseProvider(func(cmd.SimpleRunner) (*rigos.Release, error) {
		return &rigos.Release{ID: "alpine", Version: "3.18.0"}, nil
	}),
)
```

To teach rig about a new package manager, init system, or OS — using the same detection
registries rig uses internally — see **[docs/EXTENDING.md](docs/EXTENDING.md)**.

### A complete `ssh_config` parser

The [`sshconfig`](sshconfig/README.md) package is a from-scratch parser for OpenSSH's
`ssh_config` format. It's possibly currently the most complete `ssh_config` parser available for Go. It
implements every field in the [ssh_config(5)](https://man.openbsd.org/ssh_config) and supports
`Match` directives, `Include` (in lexical order, with circular-include detection), expansion of
`~`, environment variables and `TOKENS`, the `+`/`-`/`^` list-modifier prefixes
(`KexAlgorithms`, `HostKeyAlgorithms`, …), multistate fields, hostname canonicalization,
origin-based value precedence, and OpenSSH-faithful unquoting/splitting (`argv_split` ported from OpenSSH's source). Resolve
the effective settings for a host into a struct of your choosing:

```go
cfg, err := sshconfig.ConfigFor("myhost")   // reads ~/.ssh/config + system config, applies Match/Include
fmt.Println(cfg.User, cfg.IdentityFile, cfg.ProxyJump)
```

rig's native SSH transport consumes it internally, but the package can be imported standalone. See
**[sshconfig/README.md](sshconfig/README.md)** for the full feature list and a detailed
comparison with the existing Go alternatives.

### Safe shell & PowerShell command construction

The [`sh`](sh) package builds POSIX command strings with every argument escaped for you, so you
can stop hand-rolling `fmt.Sprintf` and manual quoting. `sh/shellescape` is a drop-in
replacement for [`alessio/shellescape`](https://github.com/alessio/shellescape) and adds `Split`, `Join`, `Unquote`, and `Expand`:

```go
cmd := sh.Command("grep", "-r", userInput, dir)        // each arg quoted safely
pipe := sh.CommandBuilder("journalctl").Arg("-u").Arg("k0s").
	Pipe("grep", "error").OutToFile("/tmp/err.log")    // chainable pipes & redirects
```

The [`powershell`](powershell) package does the equivalent for Windows targets — `Cmd`,
`EncodeCmd` (base64 `-EncodedCommand`), `CompressedCmd`, plus `SingleQuote`/`DoubleQuote` and
Windows-path helpers:

```go
ps := powershell.EncodeCmd("Get-Service | Where-Object Status -eq Running")
```

### Keep secrets out of logs

The [`redact`](redact) package scrubs sensitive values from strings or wraps an `io.Reader` /
`io.Writer` so credentials never reach your logs or terminal:

```go
w := redact.Writer(os.Stdout, "[REDACTED]", apiToken, password)   // io.WriteCloser
clean := redact.StringRedacter("***", secret).Redact(logLine)
```

These are the same helpers rig uses internally (e.g. the `cmd.Redact` exec option is backed by
`redact`).

## Built-in Protocols

- **SSH** — native Go SSH (`golang.org/x/crypto/ssh`) with ssh-agent and `ssh_config` support and sane defaults. Pageant / OpenSSH agent work on Windows.
- **OpenSSH** — drives the system's own `ssh` binary, reusing connections via session
  multiplexing for speed and full `ssh_config` fidelity (`ProxyJump`, GSSAPI, …).
- **WinRM** — for Windows hosts (SSH works on Windows too and is becoming the default for most installations).
- **Localhost** — treat the local machine the same as a remote host via `os/exec`.

## Documentation

- **API reference:** [pkg.go.dev/github.com/k0sproject/rig/v2](https://pkg.go.dev/github.com/k0sproject/rig/v2)
- **Migrating from v0.x:** [docs/MIGRATING-from-v0.x.md](docs/MIGRATING-from-v0.x.md)
- Runnable examples live in `example_test.go` and throughout the package tests.

## Projects using rig

- [k0sctl](https://github.com/k0sproject/k0sctl) - a [k0s](https://github.com/k0sproject/k0s) cluster bootstrapping, deployment and management tool.
- [k0smotron](https://github.com/k0sproject/k0smotron) - run Kubernetes control planes within a management cluster and with the integration of Cluster API 
- [launchpad](https://github.com/mirantis/launchpad) - automate the installation, upgrading, and resetting of [MKE](https://www.mirantis.com/software/mirantis-kubernetes-engine/) (Mirantis Kubernetes Engine) and [MCR](https://www.mirantis.com/software/mirantis-container-runtime/) (Mirantis Container Runtime) clusters on provisioned compute node resources

## License

[![FOSSA Status](https://app.fossa.com/api/projects/git%2Bgithub.com%2Fk0sproject%2Frig.svg?type=large)](https://app.fossa.com/projects/git%2Bgithub.com%2Fk0sproject%2Frig?ref=badge_large)
