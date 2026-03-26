package os

import (
	"errors"
	"slices"
	"testing"

	"github.com/k0sproject/rig/v2/rigtest"
)

func TestArch(t *testing.T) {
	tests := []struct {
		raw     string
		want    string
		wantErr error
	}{
		// Posix uname -m values
		{raw: "x86_64", want: "amd64"},
		{raw: "aarch64", want: "arm64"},
		{raw: "arm64", want: "arm64"},
		{raw: "armv7l", want: "arm"},
		{raw: "armv6l", want: "arm"},
		{raw: "i686", want: "386"},
		// additional Linux ARM variants
		{raw: "armv8l", want: "arm"},
		{raw: "armhfp", want: "arm"},
		// Windows PROCESSOR_ARCHITECTURE values
		{raw: "AMD64", want: "amd64"},
		{raw: "ARM64", want: "arm64"},
		{raw: "AARCH64", want: "arm64"},
		{raw: "x86", want: "386"},
		{raw: "I386", want: "386"},
		// Error cases
		{raw: "", wantErr: ErrArchNotDetected},
		{raw: "sparc64", wantErr: ErrUnrecognizedArch},
		{raw: "riscv64", wantErr: ErrUnrecognizedArch},
	}

	for _, tc := range tests {
		r := &Release{arch: tc.raw}
		got, err := r.Arch()
		if tc.wantErr != nil {
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("Arch(%q): got err %v, want %v", tc.raw, err, tc.wantErr)
			}
			continue
		}
		if err != nil {
			t.Errorf("Arch(%q): unexpected error: %v", tc.raw, err)
			continue
		}
		if got != tc.want {
			t.Errorf("Arch(%q): got %q, want %q", tc.raw, got, tc.want)
		}
	}
}

const ()

func TestParseReleaseFile(t *testing.T) {
	osReleaseRocky := `NAME="Rocky Linux"   
VERSION="8.9 (Green Obsidian)"
ID="rocky"
ID_LIKE="rhel centos fedora"
VERSION_ID="8.9"
PLATFORM_ID="platform:el8"
PRETTY_NAME="Rocky Linux 8.9 (Green Obsidian)"
ANSI_COLOR="0;32"
LOGO="fedora-logo-icon"
CPE_NAME="cpe:/o:rocky:rocky:8:GA"
HOME_URL="https://rockylinux.org/"
BUG_REPORT_URL="https://bugs.rockylinux.org/"
SUPPORT_END="2029-05-31"
ROCKY_SUPPORT_PRODUCT="Rocky-Linux-8"
ROCKY_SUPPORT_PRODUCT_VERSION="8.9"
REDHAT_SUPPORT_PRODUCT="Rocky Linux"
REDHAT_SUPPORT_PRODUCT_VERSION="8.9"`

	mr := rigtest.NewMockRunner()
	mr.AddCommandOutput(rigtest.Equal("uname -m"), "x86_64")
	mr.AddCommand(rigtest.HasPrefix("uname"), func(a *rigtest.A) error { return nil })
	mr.AddCommandOutput(rigtest.HasPrefix("cat /etc/os-release"), osReleaseRocky)

	osv, ok := ResolveLinux(mr)
	if !ok {
		t.Fatalf("ResolveLinux returned false")
	}

	if osv.ID != "rocky" {
		t.Errorf("ParseOSReleaseFile gave the wrong ID: '%s' != 'rocky'", osv.ID)
	}

	for _, like := range []string{"rhel", "centos", "fedora"} {
		if !slices.Contains(osv.IDLike, like) {
			t.Errorf("ParseOSReleaseFile gave the wrong ID_LIKE: contains('%s', '%s')", osv.IDLike, like)
		}
	}
	if osv.Version != "8.9" {
		t.Errorf("ParseOSReleaseFile gave the wrong VERSION: `%s` != `8.9`", osv.Version)
	}
	if osv.Name != "Rocky Linux" {
		t.Errorf("ParseOSReleaseFile gave the wrong ID: `%s != 'Rocky Linux'", osv.Name)
	}

	if osv.ExtraFields == nil {
		t.Errorf("ParseOSReleaseFile didn't recognize any extra fields: %+v", osv)
	} else if v, ok := osv.ExtraFields["ROCKY_SUPPORT_PRODUCT"]; !ok {
		t.Error("ParseOSReleaseFile did not handle the extra field for ROCKY_SUPPORT_PRODUCT")
	} else if v != "Rocky-Linux-8" {
		t.Errorf("ParseOSReleaseFile gave the wrong extra field value: '%s != 'Rocky-Linux-8'", v)
	}

	arch, err := osv.Arch()
	if err != nil {
		t.Errorf("Arch() returned unexpected error: %v", err)
	}
	if arch != "amd64" {
		t.Errorf("Arch() returned wrong value: %q != 'amd64'", arch)
	}
}
