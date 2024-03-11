package ssh

import "strings"

// A list of known ssh server version substrings that are used to aid in
// identifying if a server is windows or not.

var knownPosix = []string{
	"linux",
	"darwin",
	"bsd",
	"unix",
	"alpine",
	"ubuntu",
	"debian",
	"suse",
	"oracle",
	"rhel",
	"rocky",
	"sles",
	"fedora",
	"amzn",
	"arch",
	"centos",
	"bodhi",
	"elementary",
	"gentoo",
	"kali",
	"mageia",
	"manjaro",
	"slackware",
	"solaris",
	"illumos",
	"aix",
	"dragonfly",
}

func isKnownPosix(s string) bool {
	for _, v := range knownPosix {
		if strings.Contains(s, v) {
			return true
		}
	}
	return false
}
