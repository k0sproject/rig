package osrelease

import (
	"fmt"
	"strings"
)

// OSRelease host operating system version information
type OSRelease struct {
	// As documented in https://www.linux.org/docs/man5/os-release.html

	// Name - A string identifying the operating system, without a version component, and suitable for presentation to the user.
	// If not set, defaults to "NAME=Linux". Example: "NAME=Fedora" or "NAME="Debian GNU/Linux"".
	Name string `osrelease:"NAME"`

	// Version - A string identifying the operating system version, excluding any OS name information, possibly including a
	// release code name, and suitable for presentation to the user. This field is optional. Example:
	// "VERSION=17" or "VERSION="17 (Beefy Miracle)"".
	Version string `osrelease:"VERSION"`

	// ID - A lower-case string (no spaces or other characters outside of 0-9, a-z, ".", "_" and "-") identifying the
	// operating system, excluding any version information and suitable for processing by scripts or usage in
	// generated filenames. If not set, defaults to "ID=linux". Example: "ID=fedora" or "ID=debian".
	ID string `osrelease:"ID"`

	// IDLike - A whitespace-separated list of operating system IDs that this operating system is compatible with.
	IDLike string `osrelease:"ID_LIKE"`

	// VersionID - A lower-case string (mostly numeric, no spaces or other characters outside of 0-9, a-z, ".", "_" and "-")
	//identifying the operating system version, excluding any OS name information or release code name, and
	//suitable for processing by scripts or usage in generated filenames. This field is optional. Example:
	//"VERSION_ID=17" or "VERSION_ID=11.04".
	VersionID string `osrelease:"VERSION_ID"`

	// PrettyName - A pretty operating system name in a format suitable for presentation to the user. May or may not contain a
	// release code name or OS version of some kind, as suitable. If not set, defaults to "PRETTY_NAME="Linux"".
	// Example: "PRETTY_NAME="Fedora 17 (Beefy Miracle)"".
	PrettyName string `osrelease:"PRETTY_NAME"`

	// ANSIColor - A suggested presentation color when showing the OS name on the console. This should be specified as string
	// suitable for inclusion in the ESC [ m ANSI/ECMA-48 escape code for setting graphical rendition. This field
	// is optional. Example: "ANSI_COLOR="0;31" for red, or "ANSI_COLOR="1;34"" for light blue.
	ANSIColor string `osrelease:"ANSI_COLOR"`

	// CPEName A CPE name for the operating system, following the Common Platform Enumeration Specification[2] as
	// proposed by the MITRE Corporation. This field is optional. Example:
	// "CPE_NAME="cpe:/o:fedoraproject:fedora:17""
	CPEName string `osrelease:"CPE_NAME"`

	// HomeURL should refer to the homepage of the operating system, or alternatively some homepage
	// of the specific version of the operating system. The values should be in RFC3986 format[3], and should be
	// "http:" or "https:" URLs, and possibly "mailto:" or "tel:". Only one URL shall be listed in each setting.
	HomeURL string `osrelease:"HOME_URL"`

	// SupportURL should refer to the main support page for the operating system, if there is any. This is
	// primarily intended for operating systems which vendors provide support for.
	SupportURL string `osrelease:"SUPPORT_URL"`

	// BugReportURL should refer to the main bug reporting page for the operating system, if there is any. This is primarily intended for
	// operating systems that rely on community QA.
	BugReportURL string `osrelease:"BUG_REPORT_URL"`

	// PrivacyPolicyURL - should refer to the main privacy policy
	PrivacyPolicyURL string `osrelease:"PRIVACY_POLICY_URL"`

	// BuildID - A string uniquely identifying the system image used as the origin for a distribution (it is not updated
	// with system updates). The field can be identical between different VERSION_IDs as BUILD_ID is an only a
	// unique identifier to a specific version. Distributions that release each update as a new version would
	// only need to use VERSION_ID as each build is already distinct based on the VERSION_ID. This field is
	// optional. Example: "BUILD_ID="2013-03-20.3"" or "BUILD_ID=201303203".
	BuildID string `osrelease:"BUILD_ID"`

	// Variant - A string identifying a specific variant or edition of the operating system suitable for presentation to
	// the user. This field may be used to inform the user that the configuration of this system is subject to a
	// specific divergent set of rules or default configuration settings. This field is optional and may not be
	// implemented on all systems. Examples: "VARIANT="Server Edition"", "VARIANT="Smart Refrigerator Edition""
	// Note: this field is for display purposes only. The VARIANT_ID field should be used for making programmatic
	// decisions.
	Variant string `osrelease:"VARIANT"`

	// VariantID - A lower-case string (no spaces or other characters outside of 0-9, a-z, ".", "_" and "-"), identifying a
	// specific variant or edition of the operating system. This may be interpreted by other packages in order to
	// prefix new fields with an OS specific name in order to avoid name clashes. Applications reading this file must
	// ignore unknown fields. Example: "DEBIAN_BTS="debbugs://bugs.debian.org/""
	VariantID string `osrelease:"VARIANT_ID"`

	// Extra holds any other fields that are not defined in the spec
	Extra map[string]string
}

// String returns a printable representation of the OSRelease
func (o OSRelease) String() {
	var sb strings.Builder
	if o.ANSIColor != "" {
		sb.WriteString(fmt.Sprintf("\033[%sm", o.ANSIColor))
	}
	switch {
	case o.PrettyName != "":
		sb.WriteString(o.PrettyName)
	case o.Version != "":
		sb.WriteString(o.Version)
	case o.Name != "" && o.VersionID != "":
		sb.WriteString(fmt.Sprintf("%s %s", o.Name, o.VersionID))
	default:
		sb.WriteString(o.Name)
	}

	if o.ANSIColor != "" {
		sb.WriteString("\033[0m")
	}
}

// IsLike returns true if the OSRelease's ID Like contains the given id.
func (o OSRelease) IsLike(id string) bool {
	if o.ID == id || (o.ID != "windows" && id == "linux") {
		return true
	}
	for _, v := range strings.Split(o.IDLike, " ") {
		if v == id {
			return true
		}
	}
	return false
}
