# Extending rig

Rig detects a host's OS, init system, package manager, and privilege-escalation
mechanism automatically. When you need to teach it about something it doesn't ship like
a package manager it doesn't know (until you make a PR), you do it through the same
provider registry mechanism rig uses internally. 

> **Coming from v0.x?** This replaces the `rig/os` modules and the
> `rig/os/registry` package. The monolithic `Configurer` is gone. Its
> responsibilities are split across the per-subsystem providers described below.
> (For an application's *own* OS-dispatch table which is the closest analogue to a v0.x
> configurer, see the "OS module registry" section of
> [MIGRATING-from-v0.x.md](MIGRATING-from-v0.x.md).)

## The provider model

Every pluggable subsystem follows one shape:

- A **`Factory`** is a detection closure: `func(runner) (Impl, bool)`. It inspects the
  host and returns `(impl, true)` if it can serve this host, or `(nil, false)` to pass.
- A **`Registry`** holds an ordered list of factories. `Registry.Get(runner)` tries them
  in registration order and returns the first match (the matched factory is then moved to
  the front to speed up subsequent hosts in a multi-host run). If none match, it returns
  the subsystem's "not found" error.
- A **`With*Provider`** client option injects a registry's `Get` method into a client.

`Registry.Get` has exactly the signature the matching `With*Provider` option expects, so
they compose directly. The subsystems and their types:

| Subsystem | Package | Factory input | Produces | Inject with |
|---|---|---|---|---|
| OS release | `os` | `cmd.SimpleRunner` | `*os.Release` | `WithOSReleaseProvider` |
| Init system | `initsystem` | `cmd.ContextRunner` | `initsystem.ServiceManager` | `WithInitSystemProvider` |
| Package manager | `packagemanager` | `cmd.ContextRunner` | `packagemanager.PackageManager` | `WithPackageManagerProvider` |
| Privilege escalation | `sudo` | `cmd.Runner` | `cmd.Runner` (decorated) | `WithSudoProvider` |
| Remote filesystem | `remotefs` | `cmd.Runner` | `remotefs.FS` | `WithRemoteFSProvider` |

Each package also exposes a `DefaultRegistry()` (a memoized singleton holding rig's
built-in factories) and `NewRegistry()` (an empty one). The default client uses
`packagemanager.DefaultRegistry().Get` and similar for the other providers.

## Example: a custom package manager

A `PackageManager` is three methods:

```go
package mypkg

import (
	"context"

	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/packagemanager"
	"github.com/k0sproject/rig/v2/sh"
)

// fooManager drives a fictional "foopkg" tool.
type fooManager struct {
	runner cmd.ContextRunner
}

func (f *fooManager) Install(ctx context.Context, pkgs ...string) error {
	return f.runner.ExecContext(ctx, sh.Command("foopkg", append([]string{"install", "-y"}, pkgs...)...))
}

func (f *fooManager) Remove(ctx context.Context, pkgs ...string) error {
	return f.runner.ExecContext(ctx, sh.Command("foopkg", append([]string{"remove", "-y"}, pkgs...)...))
}

func (f *fooManager) Update(ctx context.Context) error {
	return f.runner.ExecContext(ctx, "foopkg update")
}

// RegisterFoo adds foopkg detection to a registry.
func RegisterFoo(reg *packagemanager.Registry) {
	reg.Register(func(c cmd.ContextRunner) (packagemanager.PackageManager, bool) {
		if c.IsWindows() {
			return nil, false
		}
		// Only claim the host if the tool is actually present.
		if c.ExecContext(context.Background(), "command -v foopkg") != nil {
			return nil, false
		}
		return &fooManager{runner: c}, true
	})
}
```

The detection check is the important part: a factory must return `false` for hosts it
can't serve, so the registry can fall through to the next candidate.

### Wiring it in

#### Per client

Build a registry with your factory first, add whichever built-ins you still want as fallbacks, and inject it:

```go
reg := packagemanager.NewRegistry()
mypkg.RegisterFoo(reg)              // tried first → wins when foopkg is present
packagemanager.RegisterApt(reg)     // fall back to the built-ins you care about
packagemanager.RegisterApk(reg)

client, err := rig.NewClient(
	rig.WithConnection(conn),
	rig.WithPackageManagerProvider(reg.Get),
)
```

Order matters: factories are tried in registration order, so put a factory that should
override a built-in ahead of it. (To keep every built-in as a fallback without
listing them, register your factory and then re-register the defaults or use the
global approach below if you only want to add support, not override it.)

#### Globally

Append your factory to the shared default registry from an `init()`. It becomes available to
every client built with default options. Because built-ins are registered first, yours is
used only when none of them match:

```go
func init() {
	mypkg.RegisterFoo(packagemanager.DefaultRegistry())
}
```

## Init system and OS release

The mechanism is identical; only the factory's input/output types differ.

For init system, implement `initsystem.ServiceManager` (`StartService`, `StopService`,
`EnableService`, `DisableService`, `ServiceIsRunning`, `ServiceScriptPath`), then:

```go
func RegisterMyInit(reg *initsystem.Registry) {
	reg.Register(func(c cmd.ContextRunner) (initsystem.ServiceManager, bool) {
		if c.ExecContext(context.Background(), "command -v myinitctl") != nil {
			return nil, false
		}
		return &myServiceManager{}, true
	})
}
// rig.NewClient(rig.WithConnection(conn), rig.WithInitSystemProvider(reg.Get))
```

OS releases return a populated `*rigos.Release`:

```go
import os "github.com/k0sproject/rig/v2/os"

func RegisterMyOS(reg *os.Registry) {
	reg.Register(func(c cmd.SimpleRunner) (*os.Release, bool) {
		if c.Exec("test -f /etc/myos-release") != nil {
			return nil, false
		}
		return &os.Release{ID: "myos", Version: "1.0"}, true
	})
}
// rig.NewClient(rig.WithConnection(conn), rig.WithOSReleaseProvider(reg.Get))
```

`sudo` (factory takes a `cmd.Runner` and returns a sudo-decorating `cmd.Runner`) and
`remotefs` work the same way through `WithSudoProvider` and `WithRemoteFSProvider`.

## When a one-off stub is enough

If you don't need host detection at all for tests, or to pin a value, skip the
registry and pass a provider function directly. It has the same signature as
`Registry.Get`:

```go
client, _ := rig.NewClient(
	rig.WithConnection(conn),
	rig.WithOSReleaseProvider(func(cmd.SimpleRunner) (*os.Release, error) {
		return &os.Release{ID: "alpine", Version: "3.18.0"}, nil
	}),
)
```
