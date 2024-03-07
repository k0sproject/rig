package os

import (
	"slices"
	"testing"

	"github.com/k0sproject/rig/rigtest"
)

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
}
