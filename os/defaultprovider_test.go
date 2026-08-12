package os

import (
	"testing"

	"github.com/k0sproject/rig/v2/cmd"
	ps "github.com/k0sproject/rig/v2/powershell"
	"github.com/k0sproject/rig/v2/rigtest"
)

const ubuntuOSRelease = `PRETTY_NAME="Ubuntu 22.04.5 LTS"
NAME="Ubuntu"
VERSION_ID="22.04"
ID=ubuntu
ID_LIKE=debian
`

// linuxRunner returns a runner that answers every probe the Linux resolvers
// make. osRelease may be empty to simulate a host with no os-release file,
// which is what the compat resolver exists for.
func linuxRunner(osRelease string) *rigtest.MockRunner {
	mr := rigtest.NewMockRunner()
	mr.AddCommandFailure(rigtest.Equal("uname | grep -q Darwin"), errCommandFailed)
	mr.AddCommandSuccess(rigtest.Equal("uname | grep -q Linux"))
	mr.AddCommandOutput(rigtest.Equal("uname -m"), "x86_64")

	if osRelease == "" {
		mr.AddCommandFailure(rigtest.Equal(osReleaseCommand), errCommandFailed)
	} else {
		mr.AddCommandOutput(rigtest.Equal(osReleaseCommand), osRelease)
	}

	// apt-get present, everything else absent, so compat resolves to the debian family.
	for _, entry := range packageManagerID {
		probe := rigtest.Equal("command -v " + entry.bin + " > /dev/null 2>&1")
		if entry.bin == "apt-get" {
			mr.AddCommandSuccess(probe)
		} else {
			mr.AddCommandFailure(probe, errCommandFailed)
		}
	}

	return mr
}

// windowsRunner returns a runner that answers the probes ResolveWindows makes.
func windowsRunner() *rigtest.MockRunner {
	mr := rigtest.NewMockRunner()
	mr.Windows = true
	mr.AddCommandOutput(rigtest.Equal(ps.Cmd("Get-CimInstance -ClassName Win32_OperatingSystem | Select-Object Caption, Version | ConvertTo-Json")),
		`{"Caption":"Microsoft Windows Server 2022","Version":"10.0.20348"}`)
	mr.AddCommandOutput(rigtest.Equal(ps.Cmd("$env:PROCESSOR_ARCHITECTURE")), "AMD64")

	return mr
}

// TestDefaultRegistryOrderingIsStable checks that resolving one host cannot
// change how the next one resolves, using the real DefaultRegistry rather than a
// purpose-built one.
//
// This is the sequence the bug was found on: Get used to move the factory that
// matched to the front of the list, so resolving a Windows host displaced
// ResolveLinux and left ResolveLinuxCompat -- which accepts any Linux host --
// ahead of it. Every Linux host after that was reported as ID "linux" with no
// version.
func TestDefaultRegistryOrderingIsStable(t *testing.T) {
	registry := DefaultRegistry()

	before, err := registry.Get(linuxRunner(ubuntuOSRelease))
	if err != nil {
		t.Fatalf("resolving a Linux host failed: %v", err)
	}
	if before.ID != "ubuntu" || before.Version != "22.04" {
		t.Fatalf("baseline: got ID %q version %q, want %q %q", before.ID, before.Version, "ubuntu", "22.04")
	}

	win, err := registry.Get(windowsRunner())
	if err != nil {
		t.Fatalf("resolving a Windows host failed: %v", err)
	}
	if win.ID != "windows" {
		t.Fatalf("windows host: got ID %q, want %q", win.ID, "windows")
	}

	after, err := registry.Get(linuxRunner(ubuntuOSRelease))
	if err != nil {
		t.Fatalf("resolving a Linux host after a Windows host failed: %v", err)
	}
	if after.ID != "ubuntu" || after.Version != "22.04" {
		t.Errorf("after resolving a Windows host: got ID %q version %q, want %q %q -- the compat fallback answered ahead of ResolveLinux",
			after.ID, after.Version, "ubuntu", "22.04")
	}
}

// TestCompatResolverIsOrderIndependent is the property that replaces the ordering
// rule this bug came from. Because ResolveLinuxCompat declines any host os-release
// can identify, a registry that consults it first still resolves those hosts
// correctly -- so a caller can add resolvers to a registry without having to know
// where the last-resort one sits.
func TestCompatResolverIsOrderIndependent(t *testing.T) {
	registry := NewRegistry()
	// Deliberately the wrong way round: the catch-all resolver first.
	RegisterLinuxCompat(registry)
	RegisterLinux(registry)

	rel, err := registry.Get(linuxRunner(ubuntuOSRelease))
	if err != nil {
		t.Fatalf("resolving a Linux host failed: %v", err)
	}
	if rel.ID != "ubuntu" || rel.Version != "22.04" {
		t.Errorf("compat resolver answered ahead of ResolveLinux: got ID %q version %q, want %q %q",
			rel.ID, rel.Version, "ubuntu", "22.04")
	}

	// And it still answers for a host that has no os-release, from either position.
	rel, err = registry.Get(linuxRunner(""))
	if err != nil {
		t.Fatalf("compat resolver was not reached: %v", err)
	}
	if rel.ID != "linux" {
		t.Errorf("ID: got %q, want %q", rel.ID, "linux")
	}
}

// TestDefaultRegistryStillFallsBackToCompat confirms the compat resolver is still
// reached when no specific resolver matches, which is the case it exists for: a
// host with no os-release file at all.
func TestDefaultRegistryStillFallsBackToCompat(t *testing.T) {
	rel, err := DefaultRegistry().Get(linuxRunner(""))
	if err != nil {
		t.Fatalf("compat fallback was not reached: %v", err)
	}
	if rel.ID != "linux" {
		t.Errorf("ID: got %q, want %q", rel.ID, "linux")
	}
	if len(rel.IDLike) != 1 || rel.IDLike[0] != "debian" {
		t.Errorf("IDLike: got %v, want [debian]", rel.IDLike)
	}
}

// TestRegisterDefaultsBuildsTheDefaultRegistry checks that a registry assembled
// with RegisterDefaults answers like DefaultRegistry, since that is what lets a
// caller build one of their own without listing the resolvers by hand.
func TestRegisterDefaultsBuildsTheDefaultRegistry(t *testing.T) {
	registry := NewRegistry()
	RegisterDefaults(registry)

	rel, err := registry.Get(linuxRunner(ubuntuOSRelease))
	if err != nil {
		t.Fatalf("resolving a Linux host failed: %v", err)
	}
	if rel.ID != "ubuntu" || rel.Version != "22.04" {
		t.Errorf("got ID %q version %q, want %q %q", rel.ID, rel.Version, "ubuntu", "22.04")
	}

	// The compat resolver came along with the rest and is still reached last.
	rel, err = registry.Get(linuxRunner(""))
	if err != nil {
		t.Fatalf("compat fallback was not reached: %v", err)
	}
	if rel.ID != "linux" {
		t.Errorf("ID: got %q, want %q", rel.ID, "linux")
	}
}

// myOSResolver resolves the hosts of a fleet that has no os-release file, which is
// the case ResolveLinuxCompat also claims. It stands down for hosts os-release can
// name so it does not shadow ResolveLinux in turn.
func myOSResolver(conn cmd.SimpleRunner) (*Release, bool) {
	if _, ok := readOSRelease(conn); ok {
		return nil, false
	}

	return &Release{ID: "myos", Version: "1.0"}, true
}

// TestRegisterFirstOverridesTheCompatResolver covers what RegisterFirst is for.
// ResolveLinuxCompat matches every Linux host os-release cannot name, so a
// resolver a caller appends for such a host is never reached, and they cannot fix
// that with self-exclusion because ResolveLinuxCompat is not theirs to change.
func TestRegisterFirstOverridesTheCompatResolver(t *testing.T) {
	appended := NewRegistry()
	RegisterDefaults(appended)
	appended.Register(myOSResolver)

	rel, err := appended.Get(linuxRunner(""))
	if err != nil {
		t.Fatalf("resolving a Linux host with no os-release failed: %v", err)
	}
	if rel.ID != "linux" {
		t.Errorf("appended resolver: got ID %q, want %q -- a resolver added with Register is expected to land behind the catch-all", rel.ID, "linux")
	}

	prepended := NewRegistry()
	RegisterDefaults(prepended)
	prepended.RegisterFirst(myOSResolver)

	rel, err = prepended.Get(linuxRunner(""))
	if err != nil {
		t.Fatalf("resolving a Linux host with no os-release failed: %v", err)
	}
	if rel.ID != "myos" || rel.Version != "1.0" {
		t.Errorf("got ID %q version %q, want %q %q -- the compat resolver answered ahead of one added with RegisterFirst",
			rel.ID, rel.Version, "myos", "1.0")
	}

	// A host os-release can name is still resolved by ResolveLinux.
	rel, err = prepended.Get(linuxRunner(ubuntuOSRelease))
	if err != nil {
		t.Fatalf("resolving a Linux host failed: %v", err)
	}
	if rel.ID != "ubuntu" || rel.Version != "22.04" {
		t.Errorf("got ID %q version %q, want %q %q", rel.ID, rel.Version, "ubuntu", "22.04")
	}
}
