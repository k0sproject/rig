package rig

import (
	"github.com/k0sproject/rig/initsystem"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/packagemanager"
	"github.com/k0sproject/rig/remotefs"
	"github.com/k0sproject/rig/sudo"
)

// The types here are aliased to make it easier to embed them into your own types
// without having to import the packages individually. Also, as they're all called
// "Service" in their respective packages, you would need to define type aliases
// locally to avoid name collisions.

// PackageManagerService is a type alias for packagemanager.Service
type PackageManagerService = packagemanager.Service

// InitSystemService is a type alias for initsystem.Service
type InitSystemService = initsystem.Service

// RemoteFSService is a type alias for remotefs.Service
type RemoteFSService = remotefs.Service

// OSReleaseService is a type alias for os.Service
type OSReleaseService = os.Service

// SudoService is a type alias for sudo.Service
type SudoService = sudo.Service
