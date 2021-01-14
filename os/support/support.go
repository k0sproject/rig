package support

// This file is intended to be imported for loading the OS support modules.
// If you want to only load individual OS support modules or load your own
// modules, import them in your own implementation.

import (
	// anonymous import for triggerint init()
	_ "github.com/k0sproject/rig/os"
	// anonymous import for triggerint init()
	_ "github.com/k0sproject/rig/os/linux"
	// anonymous import for triggerint init()
	_ "github.com/k0sproject/rig/os/linux/enterpriselinux"
	// anonymous import for triggerint init()
	_ "github.com/k0sproject/rig/os/windows"
	// anonymous import for triggerint init()
	_ "github.com/k0sproject/rig/os/mac"
)
