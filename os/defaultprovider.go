package os

import (
	"errors"
	"sync"

	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/plumbing"
)

var (
	// DefaultRegistry is the default OS release registry.
	DefaultRegistry = sync.OnceValue(func() *Registry {
		provider := NewRegistry()
		RegisterDefaults(provider)
		return provider
	})

	// ErrNotRecognized is returned when the host OS is not recognized.
	ErrNotRecognized = errors.New("host OS not recognized")
)

// Factory is a function that returns an OS release based on the provided runner.
type Factory = plumbing.Factory[cmd.SimpleRunner, *Release]

// Registry is a type that can determine the host OS given a runner.
type Registry = plumbing.Provider[cmd.SimpleRunner, *Release]

// ReleaseProvider is a function that returns OS release information given a runner.
type ReleaseProvider func(cmd.SimpleRunner) (*Release, error)

// NewRegistry creates a new OS release registry.
func NewRegistry() *Registry {
	return plumbing.NewProvider[cmd.SimpleRunner, *Release](ErrNotRecognized)
}

// RegisterDefaults registers the resolvers rig ships with, which is what
// DefaultRegistry holds. Use it to build a registry of your own without having to
// list them, and without missing resolvers added in later versions.
//
// The resolvers are appended, so a resolver of your own that has to take
// precedence over one of them must be registered before this call, or with
// Registry.RegisterFirst.
func RegisterDefaults(provider *Registry) {
	RegisterLinux(provider)
	RegisterWindows(provider)
	RegisterDarwin(provider)
	// Registered last because it is the last resort, but it does not depend on
	// that: it declines any host ResolveLinux can identify. See readOSRelease.
	RegisterLinuxCompat(provider)
}
