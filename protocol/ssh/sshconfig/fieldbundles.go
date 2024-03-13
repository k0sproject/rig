package sshconfig

import (
	"fmt"
	"net"
	"strings"
)

// RequiredFields holds the required fields for a Host entry in the ssh configuration. These fields need to
// exist in the configuration struct because the parsing rules look up these values. The Host field is the
// "host alias" that is used to match suitable confirguration blocks.
type RequiredFields struct {
	// Host is the host alias that is used to match suitable confirguration blocks, it holds
	// the value as it would have been given when running "ssh hostalias".
	Host StringValue

	// Specifies the user to log in as.  This can be useful when
	// a different user name is used on different machines.
	// This saves the trouble of having to remember to give the
	// user name on the command line.
	User StringValue

	// Specifies the real host name to log into.  This can be
	// used to specify nicknames or abbreviations for hosts.
	// Arguments to Hostname accept the tokens described in the
	// "TOKENS" section.  Numeric IP addresses are also
	// permitted (both on the command line and in Hostname
	// specifications).  The default is the name given on the
	// command line.
	Hostname StringValue
}

// SetUser sets the user field. If the name is empty, it sets the current system user.
func (rf *RequiredFields) SetUser(name string) error {
	if name != "" {
		if err := rf.User.SetString(name, ValueOriginOption, ""); err != nil {
			return fmt.Errorf("can't set user value %q: %w", name, err)
		}
	} else {
		if err := rf.User.SetString(username(), ValueOriginOption, ""); err != nil {
			return fmt.Errorf("can't set user value %q: %w", username(), err)
		}
	}
	return nil
}

// SetHost sets the host field. This must be set for the configuration parsing to work.
func (rf *RequiredFields) SetHost(host string) error {
	if host == "" {
		return fmt.Errorf("%w: host must be non-empty", ErrInvalidValue)
	}
	if err := rf.Host.SetString(host, ValueOriginOption, ""); err != nil {
		return fmt.Errorf("can't set host value %q: %w", host, err)
	}
	return nil
}

// GetHost returns the host field and a boolean indicating if the value was set.
func (rf *RequiredFields) GetHost() (string, bool) {
	return rf.Host.Get()
}

// SetHostname sets the hostname field.
func (rf *RequiredFields) SetHostname(hostName string) error {
	if hostName == "" {
		return fmt.Errorf("%w: can't set empty hostname", ErrInvalidValue)
	}
	if err := rf.Hostname.SetString(hostName, ValueOriginOption, ""); err != nil {
		return fmt.Errorf("can't set hostname value %q: %w", hostName, err)
	}
	return nil
}
	
var canonicalizationFields = []string{
	"canonicaldomains",
	"canonicalizehostname",
	"canonicalizemaxdots",
	"canonicalizefallbacklocal",
	"canonicalizepermittedcnames",
}

// CanonicalizationFields must exist in a config struct for hostname canonicalization to work.
type CanonicalizationFields struct {
	// When CanonicalizeHostname is enabled, this option
	// specifies the list of domain suffixes in which to search
	// for the specified destination host.
	CanonicalDomains StringListValue

	// Controls whether explicit hostname canonicalization is
	// performed.  The default, no, is not to perform any name
	// rewriting and let the system resolver handle all hostname
	// lookups.  If set to yes then, for connections that do not
	// use a ProxyCommand or ProxyJump, ssh(1) will attempt to
	// canonicalize the hostname specified on the command line
	// using the CanonicalDomains suffixes and
	// CanonicalizePermittedCNAMEs rules.  If
	// CanonicalizeHostname is set to always, then
	// canonicalization is applied to proxied connections too.
	//
	// If this option is enabled, then the configuration files
	// are processed again using the new target name to pick up
	// any new configuration in matching Host and Match stanzas.
	// A value of none disables the use of a ProxyJump host.
	CanonicalizeHostname MultiStateBoolValue

	// Specifies the maximum number of dot characters in a
	// hostname before canonicalization is disabled.  The
	// default, 1, allows a single dot (i.e.
	// hostname.subdomain).
	CanonicalizeMaxDots UintValue

	// Specifies whether to fail with an error when hostname
	// canonicalization fails.  The default, yes, will attempt
	// to look up the unqualified hostname using the system
	// resolver's search rules.  A value of no will cause ssh(1)
	// to fail instantly if CanonicalizeHostname is enabled and
	// the target hostname cannot be found in any of the domains
	// specified by CanonicalDomains.
	CanonicalizeFallbackLocal BoolValue

	// Specifies rules to determine whether CNAMEs should be
	// followed when canonicalizing hostnames.  The rules
	// consist of one or more arguments of
	// source_domain_list:target_domain_list, where
	// source_domain_list is a pattern-list of domains that may
	// follow CNAMEs in canonicalization, and target_domain_list
	// is a pattern-list of domains that they may resolve to.
	//
	// For example,
	// "*.a.example.com:*.b.example.com,*.c.example.com" will
	// allow hostnames matching "*.a.example.com" to be
	// canonicalized to names in the "*.b.example.com" or
	// "*.c.example.com" domains.
	//
	// A single argument of "none" causes no CNAMEs to be
	// considered for canonicalization.  This is the default
	// behaviour.
	CanonicalizePermittedCNAMEs StringListValue
}

// Canonicalize takes an address, applies the canonicalization rules and returns two values:
// 1. The canonicalized address - if the value is non-empty, it should be used as the canonical hostname.
// 2. A boolean indicating whether the original address should be used as is or should an error be returned.
func (cf CanonicalizationFields) Canonicalize(addr string) (string, bool) { //nolint:cyclop
	// Check if CanonicalizeHostname is enabled
	if val, ok := cf.CanonicalizeHostname.Get(); ok && val != "always" && val != boolYes && val != boolTrue {
		// when it is set to "always", then canonicalization is applied to proxied connections too
		return "", true // CanonicalizeHostname is not enabled, true means "feel free to use the addr as is"
	}

	if val, ok := cf.CanonicalizeMaxDots.Get(); ok && val > 0 {
		parts := strings.Split(addr, ".")
		if len(parts) > int(val) {
			// Too many dots, its considered canonical already.
			return "", true
		}
	}

	if val, ok := cf.CanonicalDomains.Get(); ok {
		for _, d := range val {
			if strings.HasSuffix(addr, d) {
				// The addr matches a domain in CanonicalDomains, it's canonical already.
				return "", true
			}
		}
	}

	cname, err := lookupCNAME(addr)
	if err != nil {
		if val, ok := cf.CanonicalizeFallbackLocal.Get(); ok && val {
			// Fallback enabled, feel free to use the addr as-is
			return "", true
		}
		// Fallback disabled, do not use the old address
		return "", false
	}

	if cf.isCNAMEPermitted(cname, true) {
		// Use the CNAME as the canonical hostname - dont use the old address.
		return cname, false
	}
	if val, ok := cf.CanonicalizeFallbackLocal.Get(); ok && val {
		// Fallback enabled, feel free to use the addr as-is
		return "", true
	}
	// Fallback disabled, do not use the old address
	return "", false
}

// isCNAMEPermitted checks if a given CNAME matches the rules in CanonicalizePermittedCNAMEs.
// when dst is false, the source is checked against, otherwise the destination is checked.
func (cf CanonicalizationFields) isCNAMEPermitted(cname string, dst bool) bool {
	cnames, ok := cf.CanonicalizePermittedCNAMEs.Get()
	if !ok {
		return false
	}
	if len(cnames) == 0 {
		return false
	}
	for _, rule := range cnames {
		parts := strings.Split(rule, ":")
		if len(parts) != 2 {
			continue
		}
		checkidx := 0
		if dst {
			// checking destination, not source
			checkidx = 1
		}

		match, err := matchesPattern(parts[checkidx], cname)
		if err == nil && match {
			return true
		}
	}

	return false
}

func lookupCNAME(addr string) (string, error) {
	cname, err := net.LookupCNAME(addr)
	if err != nil {
		return "", fmt.Errorf("failed to look up CNAME for %s: %w", addr, err)
	}
	return cname, nil
}
