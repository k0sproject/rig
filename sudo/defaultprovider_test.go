package sudo_test

import (
	"testing"

	"github.com/k0sproject/rig/v2/rigtest"
	"github.com/k0sproject/rig/v2/sudo"
	"github.com/stretchr/testify/require"
)

// rootRunner is a host running as root that also has a working sudo. That
// combination is the point: both RegisterUID0Noop and RegisterSudo match it, so
// which one answers is decided purely by registration order.
func rootRunner() *rigtest.MockRunner {
	mr := rigtest.NewMockRunner()
	mr.ErrDefault = errProbe
	mr.AddCommandSuccess(rigtest.Contains("id -u"))
	mr.AddCommandSuccess(rigtest.Contains("sudo -n"))
	mr.AddCommandSuccess(rigtest.Equal("whoami"))

	return mr
}

// sudoerRunner is a host that is not root but can use sudo, so RegisterSudo is
// the only factory in DefaultRegistry that matches it.
func sudoerRunner() *rigtest.MockRunner {
	mr := rigtest.NewMockRunner()
	mr.ErrDefault = errProbe
	mr.AddCommandSuccess(rigtest.Contains("sudo -n"))

	return mr
}

// TestDefaultRegistryPrefersNoopForRoot pins the registration order in
// DefaultRegistry, where RegisterUID0Noop comes before RegisterSudo so a root
// host runs commands unmodified rather than wrapping them in sudo needlessly.
//
// Because a root host with sudo installed matches both factories, this only holds
// while lookups are answered in registration order. Get used to move the factory
// that matched to the front of the list, so resolving an ordinary sudo host first
// pushed RegisterSudo ahead of RegisterUID0Noop, and every root host resolved
// after that got its commands wrapped in sudo.
func TestDefaultRegistryPrefersNoopForRoot(t *testing.T) {
	registry := sudo.DefaultRegistry()

	// Resolve a non-root sudo host first -- the lookup that used to reorder the
	// shared registry.
	sudoer, err := registry.Get(sudoerRunner())
	require.NoError(t, err)
	require.NotNil(t, sudoer)

	// A root host must still be given the noop decorator.
	mr := rootRunner()
	runner, err := registry.Get(mr)
	require.NoError(t, err)
	require.NoError(t, runner.Exec("whoami"))

	require.NoError(t, mr.Received(rigtest.Equal("whoami")),
		"root host did not run the command unmodified")
	require.NoError(t, mr.NotReceived(rigtest.Contains("sudo -n")),
		"root host was given the sudo decorator instead of noop")
}
