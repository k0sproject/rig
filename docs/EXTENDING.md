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
  in registration order and returns the first match. If none match, it returns
  the subsystem's "not found" error. A lookup never reorders the list, so what one host
  resolved to cannot change what the next one resolves to. `Register` appends;
  `RegisterFirst` puts a factory at the front, ahead of everything else.
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
built-in factories), `NewRegistry()` (an empty one) and `RegisterDefaults(reg)`
(rig's built-ins, added to a registry of yours). The default client uses
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

Build a registry with your factory first, add the built-ins behind it, and inject it:

```go
reg := packagemanager.NewRegistry()
mypkg.RegisterFoo(reg)               // tried first → wins when foopkg is present
packagemanager.RegisterDefaults(reg) // then everything rig ships

client, err := rig.NewClient(
	rig.WithConnection(conn),
	rig.WithPackageManagerProvider(reg.Get),
)
```

Order matters: factories are tried in registration order, so put a factory that should
override a built-in ahead of it. `RegisterDefaults` appends, so calling it before
`RegisterFoo` would leave your factory behind the built-ins. If you only want a few
of them, the individual `RegisterApt`/`RegisterApk`/… functions are still there —
at the cost of not picking up managers rig adds in later versions.

#### Globally

Add your factory to the shared default registry from an `init()`. It becomes available to
every client built with default options. `Register` appends, so yours is used only when
none of the built-ins match:

```go
func init() {
	mypkg.RegisterFoo(packagemanager.DefaultRegistry())
}
```

#### Overriding a built-in

Appending is not enough when a built-in matches the same hosts your factory does —
it is consulted first, so yours is never reached. `RegisterFirst` puts a factory at
the very front, ahead of everything registered before or after it.

A `RegisterFoo`-style helper picks `Register` on the caller's behalf, so export the
factory itself if you want to leave them the choice:

```go
// In mypkg, alongside RegisterFoo.
func FooFactory(c cmd.ContextRunner) (packagemanager.PackageManager, bool) {
	// ...same detection as above...
}

// In the consumer.
func init() {
	packagemanager.DefaultRegistry().RegisterFirst(mypkg.FooFactory)
}
```

This is the lever for the case rig's own factories solve by self-exclusion, where a
factory matching a superset of another's hosts declines the ones the more specific
factory handles — `os.ResolveLinuxCompat` stands down for any host `os-release` can
name, `yum` for a host with `dnf`, SysVinit for a host with systemd. That is the
better pattern where it is available, but it is only available to whoever owns the
broad factory. A caller cannot make a built-in stand down for a factory that did not
exist when it was written, and `RegisterFirst` is what they use instead.

Two things to know about it: where it is called more than once the most recent call
wins, and a resolved value is memoized per host, so register from an `init()` or
before constructing clients rather than after.

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
