package osrelease

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

var (
	validOSRelease = []byte(`PRETTY_NAME="Ubuntu 22.04.2 LTS"
NAME="Ubuntu"
VERSION_ID="22.04"
VERSION="22.04.2 LTS (Jammy Jellyfish)"
VERSION_CODENAME=jammy
ID=ubuntu
ID_LIKE=debian
HOME_URL="https://www.ubuntu.com/"
SUPPORT_URL="https://help.ubuntu.com/"
BUG_REPORT_URL="https://bugs.launchpad.net/ubuntu/"
PRIVACY_POLICY_URL="https://www.ubuntu.com/legal/terms-and-policies/privacy-policy"
UBUNTU_CODENAME=jammy
`)
)

func TestDecoder(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		osrelease, err := Decode(bytes.NewReader(validOSRelease))
		if err != nil {
			t.Errorf("Error decoding os-release: %s", err)
		}
		if osrelease.PrettyName != "Ubuntu 22.04.2 LTS" {
			t.Errorf("Unexpected PrettyName: %s", osrelease.PrettyName)
		}
		if osrelease.ID != "ubuntu" {
			t.Errorf("Unexpected ID: %s", osrelease.ID)
		}
		if osrelease.Extra["UBUNTU_CODENAME"] != "jammy" {
			t.Errorf("Unexpected Extra[UBUNTU_CODENAME}: %v", osrelease.Extra["UBUNTU_CODENAME"])
		}
	})

	t.Run("expected failure", func(t *testing.T) {
		osrelease, err := Decode(bytes.NewReader([]byte(`#\n`)))
		if osrelease != nil {
			t.Errorf("Expected nil osrelease, got: %v", osrelease)
		}
		if err == nil {
			t.Errorf("Expected an error")
		}
		if !errors.Is(err, ErrParseOSRelease) {
			t.Errorf("Unexpected error: %v", err)
		}
		if !strings.Contains(err.Error(), "missing required fields") {
			t.Errorf("Unexpected error: %v", err)
		}
	})
}
