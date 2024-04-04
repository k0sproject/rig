package sshconfig

import "github.com/k0sproject/rig/v2/sshconfig/options"

type setterFunc func(setter *Setter, key string, values ...string) error

type printerFunc func(printer *printer, key string) (string, bool)

type keyInfo struct {
	key       string      // the canonical key as it appears in the spec, such as CanonicalizePermittedCNAMEs
	setFunc   setterFunc  // which function to use to set the value for this key
	printFunc printerFunc // which function to use to print the value for this key
	tokens    []string    // the list of allowed tokens for the value of this key
	tilde     bool        // whether tilde expansion is allowed in the value of this key
	env       bool        // whether environment variable expansion is allowed in the value of this key
}

// These tokens are used to replace values in the config file, like %h for the hostname.
// the tokens are not allowed in all fields, and some fields only allow a subset of tokens.
var (
	alltokens = []string{
		"%%", "%C", "%d", "%f", "%H", "%h", "%I", "%i", "%j", "%K", "%k", "%L", "%l", "%n",
		"%p", "%r", "%T", "%t", "%u",
	}
	tokenset1 = []string{"%%", "%C", "%d", "%h", "%i", "%j", "%k", "%L", "%l", "%n", "%p", "%r", "%u"}
	tokenset2 = append(tokenset1, "%f", "%H", "%I", "%K", "%t")
	tokenset3 = []string{"%%", "%h", "%n", "%p", "%r"}
)

var ( //nolint:gofumpt
	// this is here for making the list below easier to read.
	noTokens []string
)

const (
	// these are here for making the list below easier to read.
	noTilde = false
	noEnv   = false
)

var knownKeys = map[string]keyInfo{
	// deprecated keys (from readconf.c)
	"cipher":                 {"", (*Setter).ignore, nil, noTokens, noTilde, noEnv},
	"fallbacktorsh":          {"", (*Setter).ignore, nil, noTokens, noTilde, noEnv},
	"globalknownhostsfile2":  {"", (*Setter).ignore, nil, noTokens, noTilde, noEnv},
	"identityfile2":          {"", (*Setter).ignore, nil, noTokens, noTilde, noEnv},
	"protocol":               {"", (*Setter).ignore, nil, noTokens, noTilde, noEnv},
	"pubkeyacceptedkeytypes": {"", (*Setter).ignore, nil, noTokens, noTilde, noEnv},
	"rhostsauthentication":   {"", (*Setter).ignore, nil, noTokens, noTilde, noEnv},
	"useprivilegedport":      {"", (*Setter).ignore, nil, noTokens, noTilde, noEnv},
	"userknownhostsfile2":    {"", (*Setter).ignore, nil, noTokens, noTilde, noEnv},
	"useroaming":             {"", (*Setter).ignore, nil, noTokens, noTilde, noEnv},
	"usersh":                 {"", (*Setter).ignore, nil, noTokens, noTilde, noEnv},

	// unsupported options (from readconf.c)
	"afstokenpassing":         {"", (*Setter).ignore, nil, noTokens, noTilde, noEnv},
	"compressionlevel":        {"", (*Setter).ignore, nil, noTokens, noTilde, noEnv},
	"kerberosauthentication":  {"", (*Setter).ignore, nil, noTokens, noTilde, noEnv},
	"kerberostgtpassing":      {"", (*Setter).ignore, nil, noTokens, noTilde, noEnv},
	"rhostsrsaauthentication": {"", (*Setter).ignore, nil, noTokens, noTilde, noEnv},
	"rsaauthentication":       {"", (*Setter).ignore, nil, noTokens, noTilde, noEnv},

	// config structuring keys
	fkHost:    {"Host", (*Setter).setHost, (*printer).stringer, noTokens, noTilde, noEnv},
	"match":   {"Match", nil, nil, noTokens, noTilde, noEnv},
	"include": {"Include", nil, nil, noTokens, noTilde, noEnv},

	"addkeystoagent":                   {"AddKeysToAgent", (*Setter).setAddKeysToAgentOption, (*printer).stringer, noTokens, noTilde, noEnv},
	"addressfamily":                    {"AddressFamily", enum("any", "inet", "inet6"), (*printer).stringer, noTokens, noTilde, noEnv},
	"applemultipath":                   {"AppleMultiPath", (*Setter).setBool, (*printer).boolean, noTokens, noTilde, noEnv},
	"batchmode":                        {"BatchMode", (*Setter).setBool, (*printer).boolean, noTokens, noTilde, noEnv},
	"bindaddress":                      {"BindAddress", (*Setter).setString, (*printer).stringer, noTokens, noTilde, noEnv},
	"bindinterface":                    {"BindInterface", (*Setter).setString, (*printer).stringer, noTokens, noTilde, noEnv},
	"canonicaldomains":                 {"CanonicalDomains", (*Setter).setStringSlice, (*printer).stringerslice, noTokens, noTilde, noEnv},
	"canonicalizefallbacklocal":        {"CanonicalizeFallbackLocal", (*Setter).setBool, (*printer).stringer, noTokens, noTilde, noEnv},
	"canonicalizehostname":             {"CanonicalizeHostname", extendedBoolSetter[options.CanonicalizeHostnameOption](), (*printer).stringer, noTokens, noTilde, noEnv},
	"canonicalizemaxdots":              {"CanonicalizeMaxDots", (*Setter).setInt, (*printer).number, noTokens, noTilde, noEnv},
	"canonicalizepermittedcnames":      {"CanonicalizePermittedCNAMEs", (*Setter).setColonSeparatedValues, (*printer).stringerslice, noTokens, noTilde, noEnv},
	"casignaturealgorithms":            {"CASignatureAlgorithms", preloadedDefaultsSetter(defaultList("CASignatureAlgorithms")), (*printer).stringercsv, noTokens, noTilde, noEnv},
	"certificatefile":                  {"CertificateFile", (*Setter).appendPathList, (*printer).stringer, tokenset1, true, true},
	"channeltimeout":                   {"ChannelTimeout", (*Setter).setChannelTimeoutOption, (*printer).channeltimeout, noTokens, noTilde, noEnv},
	"checkhostip":                      {"CheckHostIP", (*Setter).setBool, (*printer).boolean, noTokens, noTilde, noEnv},
	"ciphers":                          {"Ciphers", preloadedDefaultsSetter(defaultList("Ciphers")), (*printer).stringercsv, noTokens, noTilde, noEnv},
	"clearallforwardings":              {"ClearAllForwardings", (*Setter).setBool, (*printer).boolean, noTokens, noTilde, noEnv},
	"compression":                      {"Compression", (*Setter).setBool, (*printer).boolean, noTokens, noTilde, noEnv},
	"connectionattempts":               {"ConnectionAttempts", (*Setter).setInt, (*printer).number, noTokens, noTilde, noEnv},
	"connecttimeout":                   {"ConnectTimeout", (*Setter).setDuration, (*printer).duration, noTokens, noTilde, noEnv},
	"controlmaster":                    {"ControlMaster", extendedBoolSetter[options.ControlMasterOption](), (*printer).stringer, noTokens, noTilde, noEnv},
	"controlpath":                      {"ControlPath", (*Setter).setPath, (*printer).stringer, tokenset1, true, true},
	"controlpersist":                   {"ControlPersist", extendedBoolSetter[options.ControlPersistOption](), (*printer).stringer, noTokens, noTilde, noEnv},
	"dynamicforward":                   {"DynamicForward", (*Setter).appendStringList, (*printer).stringerslice, noTokens, noTilde, noEnv},
	"enableescapecommandline":          {"EnableEscapeCommandline", (*Setter).setBool, (*printer).boolean, noTokens, noTilde, noEnv},
	"enablesshkeysign":                 {"EnableSSHKeysign", (*Setter).setBool, (*printer).boolean, noTokens, noTilde, noEnv},
	"escapechar":                       {"EscapeChar", (*Setter).setEscapeCharOption, (*printer).stringer, noTokens, noTilde, noEnv},
	"exitonforwardfailure":             {"ExitOnForwardFailure", (*Setter).setBool, (*printer).boolean, noTokens, noTilde, noEnv},
	"fingerprinthash":                  {"FingerprintHash", extendedBoolSetter[options.FingerprintHashOption](), (*printer).stringer, noTokens, noTilde, noEnv},
	"forkafterauthentication":          {"ForkAfterAuthentication", (*Setter).setBool, (*printer).boolean, noTokens, noTilde, noEnv},
	"forwardagent":                     {"ForwardAgent", extendedBoolSetter[options.ForwardAgentOption](), (*printer).stringer, noTokens, noTilde, noEnv}, // envs handled in Option
	"forwardx11":                       {"ForwardX11", (*Setter).setBool, (*printer).boolean, noTokens, noTilde, noEnv},
	"forwardx11timeout":                {"ForwardX11Timeout", (*Setter).setDuration, (*printer).duration, noTokens, noTilde, noEnv},
	"forwardx11trusted":                {"ForwardX11Trusted", (*Setter).setBool, (*printer).boolean, noTokens, noTilde, noEnv},
	"gatewayports":                     {"GatewayPorts", (*Setter).setBool, (*printer).boolean, noTokens, noTilde, noEnv},
	"globalknownhostsfile":             {"GlobalKnownHostsFile", (*Setter).setPathList, (*printer).stringerslice, noTokens, noTilde, noEnv},
	"gssapiauthentication":             {"GSSAPIAuthentication", (*Setter).setBool, (*printer).boolean, noTokens, noTilde, noEnv},
	"gssapidelegatecredentials":        {"GSSAPIDelegateCredentials", (*Setter).setBool, (*printer).boolean, noTokens, noTilde, noEnv},
	"hashknownhosts":                   {"HashKnownHosts", (*Setter).setBool, (*printer).boolean, noTokens, noTilde, noEnv},
	"hostbasedkeytypes":                {"HostbasedAcceptedAlgorithms", preloadedDefaultsSetter(defaultList("HostbasedAcceptedAlgorithms")), (*printer).stringercsv, noTokens, noTilde, noEnv}, // (deprecated) alias
	"hostbasedacceptedalgorithms":      {"HostbasedAcceptedAlgorithms", preloadedDefaultsSetter(defaultList("HostbasedAcceptedAlgorithms")), (*printer).stringercsv, noTokens, noTilde, noEnv},
	"hostbasedauthentication":          {"HostbasedAuthentication", (*Setter).setBool, (*printer).boolean, noTokens, noTilde, noEnv},
	"hostkeyalgorithms":                {"HostKeyAlgorithms", preloadedDefaultsSetter(defaultList("HostKeyAlgorithms")), (*printer).stringercsv, noTokens, noTilde, noEnv},
	"hostkeyalias":                     {"HostKeyAlias", (*Setter).setString, (*printer).stringer, noTokens, noTilde, noEnv},
	"hostname":                         {"Hostname", (*Setter).setString, (*printer).stringer, []string{"%%", "%h"}, noTilde, noEnv},
	"identitiesonly":                   {"IdentitiesOnly", (*Setter).setBool, (*printer).boolean, noTokens, noTilde, noEnv},
	"identityagent":                    {"IdentityAgent", extendedBoolSetter[options.IdentityAgentOption](), (*printer).stringer, tokenset1, noTilde, noEnv},
	"identityfile":                     {"IdentityFile", (*Setter).appendPathList, (*printer).stringerslice, tokenset1, true, noEnv},
	"ignoreunknown":                    {"IgnoreUnknown", (*Setter).setStringSlice, (*printer).stringerslice, noTokens, noTilde, noEnv},
	"ipqos":                            {"IPQoS", (*Setter).setIPQoSOption, (*printer).stringerslice, noTokens, noTilde, noEnv},
	"challengeresponseauthentication":  {"KbdInteractiveAuthentication", (*Setter).setBool, (*printer).boolean, noTokens, noTilde, noEnv}, // alias
	"skeyauthentication":               {"KbdInteractiveAuthentication", (*Setter).setBool, (*printer).boolean, noTokens, noTilde, noEnv}, // alias
	"tisauthentication":                {"KbdInteractiveAuthentication", (*Setter).setBool, (*printer).boolean, noTokens, noTilde, noEnv}, // alias
	"kbdinteractiveauthentication":     {"KbdInteractiveAuthentication", (*Setter).setBool, (*printer).boolean, noTokens, noTilde, noEnv},
	"kbdinteractivedevices":            {"KbdInteractiveDevices", (*Setter).setStringSliceCSV, (*printer).stringercsv, noTokens, noTilde, noEnv},
	"kexalgorithms":                    {"KexAlgorithms", preloadedDefaultsSetter(defaultList("KexAlgorithms")), (*printer).stringercsv, noTokens, noTilde, noEnv},
	"knownhostscommand":                {"KnownHostsCommand", (*Setter).setString, (*printer).stringer, tokenset2, noTilde, noEnv},
	"localcommand":                     {"LocalCommand", (*Setter).setString, (*printer).stringer, alltokens, noTilde, noEnv},
	"localforward":                     {"LocalForward", (*Setter).setForwardOption, (*printer).forward, tokenset1, noTilde, noEnv},
	"loglevel":                         {"LogLevel", enum("QUIET", "FATAL", "ERROR", "INFO", "VERBOSE", "DEBUG", "DEBUG1", "DEBUG2", "DEBUG3"), (*printer).stringer, noTokens, noTilde, noEnv},
	"logverbose":                       {"LogVerbose", (*Setter).setStringSlice, (*printer).stringerslice, noTokens, noTilde, noEnv},
	"macs":                             {"MACs", preloadedDefaultsSetter(defaultList("MACs")), (*printer).stringercsv, noTokens, noTilde, noEnv},
	"nohostauthenticationforlocalhost": {"NoHostAuthenticationForLocalhost", (*Setter).setBool, (*printer).boolean, noTokens, noTilde, noEnv},
	"numberofpasswordprompts":          {"NumberOfPasswordPrompts", (*Setter).setInt, (*printer).number, noTokens, noTilde, noEnv},
	"obscurekeystroketiming":           {"ObscureKeystrokeTiming", extendedBoolSetter[options.ObscureKeystrokeTimingOption](), (*printer).stringer, noTokens, noTilde, noEnv},
	"passwordauthentication":           {"PasswordAuthentication", (*Setter).setBool, (*printer).boolean, noTokens, noTilde, noEnv},
	"permitlocalcommand":               {"PermitLocalCommand", (*Setter).setBool, (*printer).boolean, noTokens, noTilde, noEnv},
	"permitremoteopen":                 {"PermitRemoteOpen", (*Setter).setColonSeparatedValues, (*printer).stringerslice, noTokens, noTilde, noEnv},
	"pkcs11provider":                   {"PKCS11Provider", (*Setter).setString, (*printer).stringer, noTokens, noTilde, noEnv}, // env?
	"port":                             {"Port", (*Setter).setPort, (*printer).number, noTokens, noTilde, noEnv},
	"preferredauthentications":         {"PreferredAuthentications", (*Setter).setStringSliceCSV, (*printer).stringercsv, noTokens, noTilde, noEnv},
	"proxycommand":                     {"ProxyCommand", (*Setter).setString, (*printer).stringer, tokenset3, noTilde, noEnv},
	"proxyjump":                        {"ProxyJump", (*Setter).setStringSliceCSV, (*printer).stringercsv, tokenset3, noTilde, noEnv},
	"proxyusefdpass":                   {"ProxyUseFdpass", (*Setter).setBool, (*printer).boolean, noTokens, noTilde, noEnv},
	"pubkeyacceptedalgorithms":         {"PubkeyAcceptedAlgorithms", preloadedDefaultsSetter(defaultList("PubkeyAcceptedAlgorithms")), (*printer).stringercsv, noTokens, noTilde, noEnv},
	"dsaauthentication":                {"PubkeyAuthentication", extendedBoolSetter[options.PubkeyAuthenticationOption](), (*printer).stringer, noTokens, noTilde, noEnv}, // alias
	"pubkeyauthentication":             {"PubkeyAuthentication", extendedBoolSetter[options.PubkeyAuthenticationOption](), (*printer).stringer, noTokens, noTilde, noEnv},
	"rekeylimit":                       {"RekeyLimit", (*Setter).setRekeyLimitOption, (*printer).stringer, noTokens, noTilde, noEnv},
	"remotecommand":                    {"RemoteCommand", (*Setter).setString, (*printer).stringer, tokenset1, noTilde, noEnv},
	"remoteforward":                    {"RemoteForward", (*Setter).setForwardOption, (*printer).forward, tokenset1, noTilde, noEnv},
	"requesttty":                       {"RequestTTY", extendedBoolSetter[options.RequestTTYOption](), (*printer).stringer, noTokens, noTilde, noEnv},
	"requiredrsasize":                  {"RequiredRSASize", (*Setter).setInt, (*printer).number, noTokens, noTilde, noEnv},
	"revokedhostkeys":                  {"RevokedHostKeys", (*Setter).setPath, (*printer).stringer, tokenset1, true, true},
	"securitykeyprovider":              {"SecurityKeyProvider", (*Setter).setString, (*printer).stringer, noTokens, noTilde, true}, // env should support $var syntax?
	"sendenv":                          {"SendEnv", (*Setter).setSendEnvOption, (*printer).stringerslice, noTokens, noTilde, noEnv},
	"serveralivecountmax":              {"ServerAliveCountMax", (*Setter).setInt, (*printer).number, noTokens, noTilde, noEnv},
	"serveraliveinterval":              {"ServerAliveInterval", (*Setter).setDuration, (*printer).number, noTokens, noTilde, noEnv},
	"sessiontype":                      {"SessionType", enum("none", "subsystem", "default"), (*printer).stringer, noTokens, noTilde, noEnv},
	"setenv":                           {"SetEnv", (*Setter).setSetEnvOption, (*printer).stringerslice, noTokens, noTilde, noEnv},
	"smartcarddevice":                  {"SmartcardDevice", (*Setter).setString, (*printer).stringer, noTokens, noTilde, noEnv},
	"stdinnull":                        {"StdinNull", (*Setter).setBool, (*printer).boolean, noTokens, noTilde, noEnv},
	"streamlocalbindmask":              {"StreamLocalBindMask", (*Setter).setUint, (*printer).number, noTokens, noTilde, noEnv},
	"streamlocalbindunlink":            {"StreamLocalBindUnlink", (*Setter).setBool, (*printer).boolean, noTokens, noTilde, noEnv},
	"stricthostkeychecking":            {"StrictHostKeyChecking", extendedBoolSetter[options.StrictHostKeyCheckingOption](), (*printer).stringer, noTokens, noTilde, noEnv},
	"syslogfacility":                   {"SyslogFacility", enum("DAEMON", "USER", "AUTH", "AUTHPRIV", "LOCAL0", "LOCAL1", "LOCAL2", "LOCAL3", "LOCAL4", "LOCAL5", "LOCAL6", "LOCAL7"), (*printer).stringer, noTokens, noTilde, noEnv},
	"tag":                              {"Tag", (*Setter).setString, (*printer).stringer, noTokens, noTilde, noEnv},
	"tcpkeepalive":                     {"TCPKeepAlive", (*Setter).setBool, (*printer).boolean, noTokens, noTilde, noEnv},
	"tunnel":                           {"Tunnel", extendedBoolSetter[options.TunnelOption](), (*printer).stringer, noTokens, noTilde, noEnv},
	"tunneldevice":                     {"TunnelDevice", (*Setter).setString, (*printer).stringer, noTokens, noTilde, noEnv},
	"updatehostkeys":                   {"UpdateHostKeys", extendedBoolSetter[options.UpdateHostKeysOption](), (*printer).stringer, noTokens, noTilde, noEnv},
	"usekeychain":                      {"UseKeychain", (*Setter).setBool, (*printer).boolean, noTokens, noTilde, noEnv},
	"user":                             {"User", (*Setter).setString, (*printer).stringer, noTokens, noTilde, noEnv},
	"userknownhostsfile":               {"UserKnownHostsFile", (*Setter).setPathList, (*printer).stringerslice, tokenset1, true, true},
	"verifyhostkeydns":                 {"VerifyHostKeyDNS", extendedBoolSetter[options.VerifyHostKeyDNSOption](), (*printer).stringer, noTokens, noTilde, noEnv},
	"visualhostkey":                    {"VisualHostKey", (*Setter).setBool, (*printer).boolean, noTokens, noTilde, noEnv},
	"xauthlocation":                    {"XAuthLocation", (*Setter).setPath, (*printer).stringer, noTokens, noTilde, noEnv},
}
