package os

import (
	"errors"
	"testing"

	"github.com/k0sproject/rig/v2/rigtest"
)

func setupCompatRunner(pm string) *rigtest.MockRunner {
	mr := rigtest.NewMockRunner()
	mr.AddCommand(rigtest.HasPrefix("uname"), func(_ *rigtest.A) error { return nil })
	mr.AddCommandOutput(rigtest.Equal("uname -m"), "x86_64")
	// No os-release: the case the compat resolver exists for. With one present it
	// stands down for ResolveLinux instead.
	mr.AddCommandFailure(rigtest.Equal(osReleaseCommand), errCommandFailed)
	for _, entry := range packageManagerID {
		if entry.bin == pm {
			mr.AddCommand(rigtest.Equal("command -v "+entry.bin+" > /dev/null 2>&1"), func(_ *rigtest.A) error { return nil })
		} else {
			mr.AddCommandFailure(rigtest.Equal("command -v "+entry.bin+" > /dev/null 2>&1"), errCommandFailed)
		}
	}
	return mr
}

var errCommandFailed = errors.New("not found")

func TestResolveLinuxCompatUnambiguous(t *testing.T) {
	cases := []struct {
		pm   string
		id   string
		name string
	}{
		{"apk", "alpine", "Alpine Linux"},
		{"pacman", "arch", "Arch Linux"},
		{"emerge", "gentoo", "Gentoo"},
		{"xbps-install", "void", "Void Linux"},
	}
	for _, tc := range cases {
		t.Run(tc.pm, func(t *testing.T) {
			mr := setupCompatRunner(tc.pm)
			r, ok := ResolveLinuxCompat(mr)
			if !ok {
				t.Fatal("ResolveLinuxCompat returned false")
			}
			if r.ID != tc.id {
				t.Errorf("ID: got %q, want %q", r.ID, tc.id)
			}
			if r.Name != tc.name {
				t.Errorf("Name: got %q, want %q", r.Name, tc.name)
			}
			if len(r.IDLike) != 0 {
				t.Errorf("IDLike should be empty for unambiguous entry, got %v", r.IDLike)
			}
		})
	}
}

func TestResolveLinuxCompatAmbiguous(t *testing.T) {
	cases := []struct {
		pm     string
		idLike []string
	}{
		{"dnf", []string{"rhel", "fedora"}},
		{"yum", []string{"rhel", "centos", "fedora"}},
		{"zypper", []string{"suse", "opensuse"}},
		{"apt-get", []string{"debian"}},
	}
	for _, tc := range cases {
		t.Run(tc.pm, func(t *testing.T) {
			mr := setupCompatRunner(tc.pm)
			r, ok := ResolveLinuxCompat(mr)
			if !ok {
				t.Fatal("ResolveLinuxCompat returned false")
			}
			if r.ID != "linux" {
				t.Errorf("ID: got %q, want %q (ambiguous entry must not claim specific distro)", r.ID, "linux")
			}
			if len(r.IDLike) != len(tc.idLike) {
				t.Errorf("IDLike: got %v, want %v", r.IDLike, tc.idLike)
				return
			}
			for i, v := range tc.idLike {
				if r.IDLike[i] != v {
					t.Errorf("IDLike[%d]: got %q, want %q", i, r.IDLike[i], v)
				}
			}
		})
	}
}

func TestResolveLinuxCompatNoPackageManager(t *testing.T) {
	mr := rigtest.NewMockRunner()
	mr.AddCommand(rigtest.HasPrefix("uname"), func(_ *rigtest.A) error { return nil })
	mr.AddCommandOutput(rigtest.Equal("uname -m"), "x86_64")
	mr.AddCommandFailure(rigtest.Equal(osReleaseCommand), errCommandFailed)
	for _, entry := range packageManagerID {
		mr.AddCommandFailure(rigtest.Equal("command -v "+entry.bin+" > /dev/null 2>&1"), errCommandFailed)
	}
	r, ok := ResolveLinuxCompat(mr)
	if !ok {
		t.Fatal("ResolveLinuxCompat returned false")
	}
	if r.ID != "linux" {
		t.Errorf("ID: got %q, want %q", r.ID, "linux")
	}
	if len(r.IDLike) != 0 {
		t.Errorf("IDLike should be empty when no package manager found, got %v", r.IDLike)
	}
}

// TestResolveLinuxCompatDefersToOSRelease is the self-exclusion that removes the
// need for any ordering rule between the two Linux resolvers: on a host whose
// os-release names the distribution, the compat resolver must decline so
// ResolveLinux answers, no matter which of them is consulted first.
func TestResolveLinuxCompatDefersToOSRelease(t *testing.T) {
	mr := rigtest.NewMockRunner()
	mr.AddCommand(rigtest.HasPrefix("uname"), func(_ *rigtest.A) error { return nil })
	mr.AddCommandOutput(rigtest.Equal("uname -m"), "x86_64")
	mr.AddCommandOutput(rigtest.Equal(osReleaseCommand), ubuntuOSRelease)

	if _, ok := ResolveLinuxCompat(mr); ok {
		t.Error("ResolveLinuxCompat answered for a host os-release can identify")
	}
	// It must decide that from os-release alone, without probing package managers.
	if err := mr.NotReceived(rigtest.HasPrefix("command -v")); err != nil {
		t.Errorf("compat resolver probed package managers before standing down: %v", err)
	}
}

func TestResolveLinuxCompatNotLinux(t *testing.T) {
	mr := rigtest.NewMockRunner()
	mr.AddCommandFailure(rigtest.HasPrefix("uname"), errCommandFailed)
	_, ok := ResolveLinuxCompat(mr)
	if ok {
		t.Error("ResolveLinuxCompat should return false for non-Linux")
	}
}
