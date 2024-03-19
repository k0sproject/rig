// Package sshconfig provides a parser for openssh ssh_config files.
package sshconfig

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/k0sproject/rig/v2/homedir"
	"github.com/k0sproject/rig/v2/log"
	"github.com/k0sproject/rig/v2/sh/shellescape"
)

/*
  SSH Configuration files parsing rules:

  Config value load order:

  When a configuration file is not supplied (ssh -F path or NewParser(nil)):
  1. Configuration defaults - any values set here can be overridden at any stage.
  2. Command-line options - any values set here have the highest precedence.
  3. User configuration file (~/.ssh/config)
  4. Global configuration file (/etc/ssh/ssh_config))
  5. Canonicalization if enabled
  6. Final match blocks

  If a configuration file path is provided (or NewParser(reader) is used)
  the stages 3 and 4 are replaced by parsing the supplied configuration file.

 Each stage can contain Include directives, which are processed as they
 are encountered.

 For each parameter, the first obtained value will be used except
 for some fields that can be used multiple times or can modify
 existing values (remove items from lists of ciphers, append to lists,
 etc)
*/

var (
	// ErrInvalidObject is returned when the object passed to Parse is not compatible.
	ErrInvalidObject = errors.New("invalid object")

	// ErrSyntax is returned when the config file has a syntax error.
	ErrSyntax = errors.New("syntax error")

	// ErrCanonicalizationFailed is returned when a hostname could not be canonicalized.
	ErrCanonicalizationFailed = errors.New("canonicalization failed")

	// ErrNotImplemented is returned when encountering a feature that is not implemented.
	ErrNotImplemented = errors.New("not implemented")

	// if the host alias gets canonicalized, the parser needs to start over.
	errRedo = errors.New("redo after canonicalization")
)

// Parser is a parser for openssh ssh_config files. It doesn't parse the full config
// to a struct, but instead it sets values to the passed in object for each directive
// it finds to match the host alias found in the object.
type Parser struct {
	// GlobalConfigPath can be changed to point to a different global config file than the /etc/ssh/ssh_config.
	GlobalConfigPath string

	// UserConfigPath can be changed to point to a different user config file than the ~/.ssh/config.
	UserConfigPath string

	// ErrorOnUnknown when true, will cause the parser to respect the `IgnoreUnknown` directive.
	ErrorOnUnknown bool

	r               *bytes.Reader
	originalHost    string
	rname           string
	canonicalized   map[string]string
	didCanonicalize bool // if the hostname field was canonicalized
	hasFinal        bool // if the config has a final directive
	doingFinal      bool // if the parser is currently parsing a final directive
}

// NewParser returns a new Parser. If r is nil, the global and user config files are read instead.
// When r is not nil, the settings from the reader will be applied over default values.
// Note that the reader will be read in its entirety into a buffer when it is not a bytes.Reader.
// This has to be done because the ssh config parsing rules require the file to be read multiple times.
func NewParser(input io.Reader) (*Parser, error) {
	parser := &Parser{}
	if input == nil {
		return parser, nil
	}
	if nr, ok := input.(*os.File); ok {
		parser.rname = nr.Name()
	} else {
		parser.rname, _ = homedir.Expand("~/.ssh/unknown")
	}
	if br, ok := input.(*bytes.Reader); ok {
		parser.r = br
		return parser, nil
	}
	buf, err := io.ReadAll(input)
	if err != nil {
		return nil, fmt.Errorf("read content to buffer: %w", err)
	}
	parser.r = bytes.NewReader(buf)
	return parser, nil
}

// Only a subset of fields are allowed inside a Match block.
var allowedInMatchFields = map[string]struct{}{
	"acceptenv":                       {},
	"allowagentforwarding":            {},
	"allowgroups":                     {},
	"allowstreamlocalforwarding":      {},
	"allowtcpforwarding":              {},
	"allowusers":                      {},
	"authenticationmethods":           {},
	"authorizedkeyscommand":           {},
	"authorizedkeyscommanduser":       {},
	"authorizedkeysfile":              {},
	"authorizedprincipalscommand":     {},
	"authorizedprincipalscommanduser": {},
	"authorizedprincipalsfile":        {},
	"banner":                          {},
	"casignaturealgorithms":           {},
	"channeltimeout":                  {},
	"chrootdirectory":                 {},
	"clientalivecountmax":             {},
	"clientaliveinterval":             {},
	"denygroups":                      {},
	"denyusers":                       {},
	"disableforwarding":               {},
	"exposeauthinfo":                  {},
	"forcecommand":                    {},
	"gatewayports":                    {},
	"gssapiauthentication":            {},
	"hostbasedacceptedalgorithms":     {},
	"hostbasedauthentication":         {},
	"hostbasedusesnamefrompacketonly": {},
	"ignorerhosts":                    {},
	"include":                         {},
	"ipqos":                           {},
	"kbdinteractiveauthentication":    {},
	"kerberosauthentication":          {},
	"loglevel":                        {},
	"maxauthtries":                    {},
	"maxsessions":                     {},
	"passwordauthentication":          {},
	"permitemptypasswords":            {},
	"permitlisten":                    {},
	"permitopen":                      {},
	"permitrootlogin":                 {},
	"permittty":                       {},
	"permittunnel":                    {},
	"permituserrc":                    {},
	"pubkeyacceptedalgorithms":        {},
	"pubkeyauthentication":            {},
	"pubkeyauthoptions":               {},
	"rekeylimit":                      {},
	"revokedkeys":                     {},
	"rdomain":                         {},
	"setenv":                          {},
	"streamlocalbindmask":             {},
	"streamlocalbindunlink":           {},
	"trustedusercakeys":               {},
	"unusedconnectiontimeout":         {},
	"x11displayoffset":                {},
	"x11forwarding":                   {},
	"x11uselocalhost":                 {},
}

func allowedInMatch(fieldname string) bool {
	_, ok := allowedInMatchFields[fieldname]
	return ok
}

// These tokens are used to replace values in the config file, like %h for the hostname.
// the tokens are not allowed in all fields, and some fields only allow a subset of tokens.
var (
	alltokens = []string{
		"%%", "%C", "%d", "%f", "%H", "%h", "%I", "%i", "%j", "%K", "%k", "%L", "%l", "%n",
		"%p", "%r", "%T", "%t", "%u",
	}
	tokenset1     = []string{"%%", "%C", "%d", "%h", "%i", "%j", "%k", "%L", "%l", "%n", "%p", "%r", "%u"}
	tokenset2     = append(tokenset1, "%f", "%H", "%I", "%K", "%t")
	tokenset3     = []string{"%%", "%h", "%n", "%p", "%r"}
	allowedTokens = map[string][]string{
		"knownhostscommand":  tokenset2,
		"hostname":           {"%%", "%h"},
		"localcommand":       alltokens,
		"proxycommand":       tokenset3,
		"proxyjump":          tokenset3,
		"certificatefile":    tokenset1,
		"controlpath":        tokenset1,
		"identityagent":      tokenset1,
		"identityfile":       tokenset1,
		"localforward":       tokenset1,
		"match exec":         tokenset1,
		"remotecommand":      tokenset1,
		"remoteforward":      tokenset1,
		"revokedhostkeys":    tokenset1,
		"userknownhostsfile": tokenset1,
	}
)

// Each field supports a different set of tokens. This function answers
// the question "is this token allowed in a field with this name?".
func isAllowedToken(fieldname, token string) bool {
	tokens, ok := allowedTokens[fieldname]
	if !ok {
		return false
	}
	for _, t := range tokens {
		if t == token {
			return true
		}
	}
	return false
}

// Some fields don't support tokens at all.
func supportsTokens(fieldname string) bool {
	_, ok := allowedTokens[fieldname]
	return ok
}

func expandToken(token string, fields map[string]configValue) string { //nolint:cyclop
	switch token {
	case "%%":
		return "%"
	case "%u":
		return username()
	case "%d":
		return home()
	case "%h":
		// TODO this casting of setters into different valuetypes is a bit ugly
		// and repetitive.
		for _, fn := range []string{"hostname", fkHost} {
			if f, ok := fields[fn]; ok {
				if f.IsSet() {
					return f.String()
				}
			}
		}
	case "%p":
		if f, ok := fields["port"]; ok {
			return f.String()
		}
	case "%n":
		if f, ok := fields[fkHost]; ok {
			return f.String()
		}
	case "%r":
		if f, ok := fields["user"]; ok {
			return f.String()
		}
	case "%j":
		if f, ok := fields["proxyjump"]; ok {
			return f.String()
		}
	}
	// unsupported or unknown token
	return token
}

type configValue interface {
	SetString(val string, originType ValueOriginType, origin string) error
	IsDefault() bool
	IsSet() bool
	String() string
}

// extractFields uses reflection to find all the configValue fields in the object.
func extractFields(v reflect.Value, t reflect.Type, fields map[string]configValue) {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		fieldVal := v.Field(i)

		// If the field is an embedded struct, process it separately
		if field.Anonymous {
			embeddedFieldVal := fieldVal

			// Ensure embeddedFieldVal is set to a struct, not a pointer
			if embeddedFieldVal.Kind() == reflect.Ptr {
				if embeddedFieldVal.IsNil() {
					embeddedFieldVal = reflect.New(embeddedFieldVal.Type().Elem()) // Create a new instance
					fieldVal.Set(embeddedFieldVal)
				}
				embeddedFieldVal = embeddedFieldVal.Elem() // Dereference the pointer
			}

			extractFields(embeddedFieldVal, embeddedFieldVal.Type(), fields)
			continue
		}

		// Prepare the field value for interface assertion
		if fieldVal.CanInterface() {
			if fieldVal.Kind() != reflect.Ptr && fieldVal.CanAddr() {
				fieldVal = fieldVal.Addr() // Get address if it's a non-pointer struct field
			}

			if setter, ok := fieldVal.Interface().(configValue); ok {
				fields[strings.ToLower(field.Name)] = setter
			}
		}
	}
}

// objFields returns a map of configValue fields in the object.
func objFields(obj any) (map[string]configValue, error) {
	fields := make(map[string]configValue)
	v := reflect.ValueOf(obj)
	t := reflect.TypeOf(obj)

	if v.Kind() == reflect.Ptr && !v.IsNil() {
		v = v.Elem()
		t = t.Elem()
	}

	extractFields(v, t, fields)

	if err := checkRequiredFields(fields); err != nil {
		return nil, err
	}

	return fields, nil
}

// these fields are required to exist in the object passed to Parse
// because some of the config directives need to look into their value.
func checkRequiredFields(fields map[string]configValue) error {
	// Validate the required fields
	if _, ok := fields["user"]; !ok {
		return fmt.Errorf("%w: user field not found in object", ErrInvalidObject)
	}
	if _, ok := fields[fkHost]; !ok {
		return fmt.Errorf("%w: host field not found in object", ErrInvalidObject)
	}
	if _, ok := fields["hostname"]; !ok {
		return fmt.Errorf("%w: hostname field not found in object", ErrInvalidObject)
	}
	return nil
}

// tokenizeRow splits a line into a key and a value.
// any comments are stripped from the value.
//
// note that comments are parsed in a different way
// depending on if the key and value are separated with
// a space or an equals sign.
//
// for example here the comment becomes part of the
// value:
//
// IdentityFile ~/.ssh/id_rsa # foo
//
// but here it doesn't:
//
// IdentityFile=~/.ssh/id_rsa # foo.
func tokenizeRow(s string) (key string, values []string, err error) {
	// find the first non-space character
	idx := strings.IndexFunc(s, func(r rune) bool { return !unicode.IsSpace(r) })

	// skip comments
	if idx == -1 || s[idx] == '#' {
		return "", nil, nil
	}

	leftTrimmed := s[idx:]

	// find separator
	idx = strings.IndexFunc(leftTrimmed, func(r rune) bool { return r == ' ' || r == '=' || r == '\t' })

	// if there is no separator, the line is invalid
	if idx == -1 {
		return "", nil, fmt.Errorf("%w: missing separator: %q", ErrSyntax, s)
	}

	key = strings.ToLower(leftTrimmed[:idx])
	if len(leftTrimmed) < idx+1 {
		return "", nil, fmt.Errorf("%w: missing value: %q", ErrSyntax, s)
	}

	//modeEquals := leftTrimmed[idx] == '='

	values, err = SplitArgs(leftTrimmed[idx+1:], true)
	if err != nil {
		return "", nil, fmt.Errorf("%w: %w", ErrSyntax, err)
	}

	if len(values) == 0 {
		return "", nil, fmt.Errorf("%w: missing value: %q", ErrSyntax, s)
	}

	return key, values, nil
}

// matchesPattern compares a single pattern against a string.
func matchesPattern(pattern, value string) (bool, error) {
	if pattern == "*" {
		return true, nil
	}

	if !strings.ContainsAny(pattern, "*?") {
		return pattern == value, nil
	}

	var sb strings.Builder
	sb.WriteString("^")
	for _, ch := range pattern {
		switch ch {
		case '*':
			sb.WriteString(".*")
		case '?':
			sb.WriteString(".")
		default:
			if !unicode.IsLetter(ch) && !unicode.IsNumber(ch) {
				sb.WriteRune('\\')
			}
			sb.WriteRune(ch)
		}
	}
	sb.WriteString("$")

	regex, err := regexp.Compile(sb.String())
	if err != nil {
		return false, fmt.Errorf("invalid pattern: %w", err)
	}

	return regex.MatchString(value), nil
}

// matchesMatch parses the Match directive's value and determines if it applies to the current context.
func (p *Parser) matchesMatch(conditions []string, fields map[string]configValue) (bool, error) { //nolint:gocognit,cyclop,funlen,gocyclo // TODO split this up
	for i := 0; i < len(conditions); i++ {
		statement := strings.ToLower(conditions[i])

		if statement == "all" {
			if len(conditions) > 1 {
				return false, fmt.Errorf("%w: match condition %q must be alone", ErrSyntax, statement)
			}
			return true, nil
		}

		if statement == "canonical" {
			if !p.didCanonicalize {
				return false, nil
			}
		}

		if statement == "final" {
			p.hasFinal = true
			if !p.doingFinal {
				return false, nil
			}
		}

		if i+1 >= len(conditions) {
			return false, fmt.Errorf("%w: incomplete Match condition: %q", ErrSyntax, conditions)
		}

		// consume the directive value
		i++
		conditionValues := strings.Split(conditions[i], ",")

		switch statement {
		case "user":
			userValue, ok := fields["user"].(*StringValue)
			if !ok {
				return false, fmt.Errorf("%w: user field not found or is not a StringValue", ErrInvalidObject)
			}
			user, ok := userValue.Get()
			if !ok {
				return false, fmt.Errorf("%w: user not set for evaluating match condition", ErrInvalidObject)
			}
			match, err := matchesPatterns(conditionValues, user)
			if err != nil {
				return false, fmt.Errorf("match user for match condition: %w", err)
			}
			if !match {
				return false, nil
			}
		case "originalhost":
			if p.originalHost == "" {
				return false, fmt.Errorf("%w: originalhost not set for evaluating match condition", ErrInvalidObject)
			}
			match, err := matchesPatterns(conditionValues, p.originalHost)
			if err != nil {
				return false, fmt.Errorf("match originalhost for match condition: %w", err)
			}
			if !match {
				return false, nil
			}
		case fkHost:
			hostValue, ok := fields[fkHost]
			if !ok {
				return false, fmt.Errorf("%w: host field not found", ErrInvalidObject)
			}
			host := hostValue.String()
			match, err := matchesPatterns(conditionValues, host)
			if err != nil {
				return false, fmt.Errorf("match host for match condition: %w", err)
			}
			if !match {
				return false, nil
			}
		case "exec":
			cmd := exec.Command(conditionValues[0], conditionValues[1:]...) //nolint:gosec // yes, this runs arbitrary commands
			if err := cmd.Run(); err != nil {
				return false, nil //nolint:nilerr // error content is not relevant
			}
		case "address":
			// TODO there needs to be some function in the config to get the address.
			// this can be a pattern or a CIDR
			return false, nil
		case "group":
			user, err := user.Current()
			if err != nil {
				return false, fmt.Errorf("can't get current user: %w", err)
			}
			groups, err := user.GroupIds()
			if err != nil {
				return false, fmt.Errorf("can't get current user groups: %w", err)
			}
			for _, group := range groups {
				match, err := matchesPatterns(conditionValues, group)
				if err != nil {
					return false, fmt.Errorf("match group for match condition: %w", err)
				}
				if !match {
					return false, nil
				}
			}
		case "tagged":
			hostTagValue, ok := fields["tag"]
			if !ok {
				return false, nil
			}
			match, err := matchesPatterns(conditionValues, hostTagValue.String())
			if err != nil {
				return false, fmt.Errorf("match tag for match condition: %w", err)
			}
			if !match {
				return false, nil
			}
		case "localaddress", "localport", "rdomain":
			return false, fmt.Errorf("%w: match condition %q not supported by the ssh config parser", ErrNotImplemented, statement)
		}
	}
	return true, nil
}

// check if the value matches the patterns.
// the rule is that !negated patterns alone will never yield a
// match unless there is also a positive match in the pattern.
func matchesPatterns(patterns []string, value string) (bool, error) {
	var hasPositiveMatch bool
	for _, pattern := range patterns {
		if pattern == "" {
			continue
		}
		subPatterns := strings.Split(pattern, ",")
		for _, subPattern := range subPatterns {
			subPattern = strings.TrimSpace(subPattern)
			if subPattern == "" {
				continue
			}
			negate := strings.HasPrefix(pattern, "!")
			if negate {
				pattern = pattern[1:]
			}

			match, err := matchesPattern(pattern, value)
			if err != nil {
				return false, err
			}

			if match {
				if negate {
					return false, nil
				}
				hasPositiveMatch = true
			}
		}
	}

	return hasPositiveMatch, nil
}

var tokenRe = regexp.MustCompile(`%[a-zA-Z%]`)

type withAttrs interface {
	log.TraceLogger
	With(attrs ...any) *slog.Logger
}

func (p *Parser) parse(obj withRequiredFields, fields map[string]configValue, reader io.Reader, originType ValueOriginType, origin string, appliesToCurrent bool) error { //nolint:cyclop,gocognit,funlen,gocyclo,maintidx // TODO a pretty strong hint to split this up
	host, ok := obj.GetHost()
	if !ok {
		return fmt.Errorf("%w: host field is not set", ErrInvalidObject)
	}
	if p.originalHost == "" {
		p.originalHost = host
	}
	inMatch := false

	scanner := bufio.NewScanner(reader)
	ctx := context.Background()

	var row int
	var trace log.TraceLogger
	tlog := log.GetTraceLogger()
	for scanner.Scan() {
		row++
		key, values, err := tokenizeRow(scanner.Text())
		if err != nil {
			return fmt.Errorf("parse row %d: %w", row, err)
		}
		if key == "" {
			continue
		}
		var orig string
		if originType == ValueOriginDefault {
			orig = "defaults"
		} else {
			orig = origin
		}
		if tl, ok := tlog.(withAttrs); ok {
			trace = tl.With("origin", orig, "origintype", originType, "hostalias", host, "row", row, "key", key)
		} else {
			trace = tlog
		}

		trace.Log(ctx, slog.LevelInfo, "parsing")

		if supportsTokens(key) {
			trace.Log(ctx, slog.LevelInfo, "field supports token expansion")
			for i, value := range values {
				values[i] = tokenRe.ReplaceAllStringFunc(value, func(token string) string {
					if isAllowedToken(key, token) {
						expanded := expandToken(token, fields)
						trace.Log(ctx, slog.LevelInfo, "expanded token", "token", token, "expanded", expanded)
						return expanded
					}
					trace.Log(ctx, slog.LevelInfo, "token unknown or not allowed", "token", token)
					return token
				})
			}
		}

		switch key {
		case "certificatefile", "controlpath", "identityagent", "identityfile", "knownhostscommand", "userknownhostsfile":
			for i, value := range values {
				val, err := expand(value)
				if err != nil {
					return fmt.Errorf("expand value %q for %q in %s:%d: %w", value, key, origin, row, err)
				}
				values[i] = val
			}
		case "localforward", "remoteforward":
			// these fields only accept a socket path from expansion
			for i, value := range values {
				val, err := expand(value)
				if err != nil {
					return fmt.Errorf("expand value %q for %q in %s:%d: %w", value, key, origin, row, err)
				}
				unq, err := shellescape.Unquote(val)
				if err != nil {
					return fmt.Errorf("unquote value %q for %q in %s:%d: %w", val, key, origin, row, err)
				}
				if _, err := os.Stat(unq); err != nil {
					return fmt.Errorf("stat value %q for %q in %s:%d: %w", unq, key, origin, row, err)
				}
				values[i] = unq
			}
		}

		switch key {
		case fkHost:
			if p.didCanonicalize {
				trace.Log(ctx, slog.LevelInfo, "canonicalization was done during the last block before Host")
				p.didCanonicalize = false
				if err := p.canonicalize(obj); err != nil {
					return fmt.Errorf("canonicalize host %q in %s:%d: %w", host, origin, row, err)
				}
			}
			inMatch = false
			match, err := matchesPatterns(values, host)
			if err != nil {
				return err
			}
			trace.Log(ctx, slog.LevelInfo, "evaluated host block conditions", "conditions", values, "applies", match)
			appliesToCurrent = match
		case "include":
			hostBefore, _ := obj.GetHost()
			if !filepath.IsAbs(values[0]) {
				values[0] = filepath.Join(filepath.Dir(origin), values[0])
			}
			matches, err := filepath.Glob(values[0])
			if err != nil {
				return fmt.Errorf("can't glob Include path %q in %s:%d: %w", values[0], origin, row, err)
			}
			for _, match := range matches {
				f, err := os.Open(match)
				if err != nil {
					return fmt.Errorf("can't open Include file %q in %s:%d: %w", match, origin, row, err)
				}
				defer f.Close()
				if err := p.parse(obj, fields, f, ValueOriginFile, match, appliesToCurrent); err != nil {
					return fmt.Errorf("failed to parse Include file %s in %s:%d: %w", match, origin, row, err)
				}
				hostAfter, _ := obj.GetHost()
				if hostBefore != hostAfter {
					trace.Log(ctx, slog.LevelInfo, "host alias changed during Include, triggering a redo", "before", hostBefore, "after", hostAfter)
					return errRedo
				}
			}
		case "match":
			if p.didCanonicalize {
				trace.Log(ctx, slog.LevelInfo, "canonicalization was done during the last block before Match")
				p.didCanonicalize = false
				if err := p.canonicalize(obj); err != nil {
					return fmt.Errorf("canonicalize host %q in %s:%d: %w", host, origin, row, err)
				}
			}
			inMatch = true
			if values[0] == "all" {
				appliesToCurrent = true
				break
			}

			matches, err := p.matchesMatch(values, fields)
			if err != nil {
				return fmt.Errorf("can't parse Match directive %q in %s:%d: %w", values, origin, row, err)
			}
			trace.Log(ctx, slog.LevelInfo, "evaluated match conditions", "conditions", values, "applies", matches)
			appliesToCurrent = matches
		case "proxyjump":
			// proxyjump and proxycommand are mutually exclusive, the first one to be set wins.
			if !appliesToCurrent {
				continue
			}

			pjump, ok := fields["proxyjump"]
			if !ok {
				continue
			}

			pcmd, ok := fields["proxycommand"]
			if !ok {
				continue
			}

			if !pcmd.IsDefault() {
				continue
			}

			if err := pjump.SetString(shellescape.QuoteCommand(values), originType, origin); err != nil {
				return fmt.Errorf("set value %q for proxycommand in %s:%d: %w", values, origin, row, err)
			}
		case "proxycommand":
			// proxyjump and proxycommand are mutually exclusive, the first one to be set wins.
			if !appliesToCurrent {
				continue
			}

			pcmd, ok := fields["proxycommand"]
			if !ok {
				continue
			}

			pjump, ok := fields["proxyjump"]
			if !ok {
				continue
			}
			if !pjump.IsDefault() {
				continue
			}

			if err := pcmd.SetString(shellescape.QuoteCommand(values), originType, origin); err != nil {
				return fmt.Errorf("set value %q for proxycommand in %s:%d: %w", values, origin, row, err)
			}
		default:
			if !appliesToCurrent {
				continue
			}
			if inMatch && !allowedInMatch(key) {
				return fmt.Errorf("%w: field %q not allowed inside match block in %s:%d", ErrSyntax, key, origin, row)
			}

			if slices.Contains(canonicalizationFields, key) {
				trace.Log(ctx, slog.LevelInfo, "touched canonicalization field")
				p.didCanonicalize = true
			}

			if f, ok := fields[key]; ok { //nolint:nestif
				trace.Log(ctx, slog.LevelInfo, "setting value", "value", values)
				if err := f.SetString(strings.Join(values, " "), originType, origin); err != nil {
					return fmt.Errorf("set value for %q in %s:%d: %w", key, origin, row, err)
				}
			} else if p.ErrorOnUnknown {
				iu, ok := fields["ignoreunknown"]
				if !ok || !iu.IsSet() || iu.String() == "" {
					return fmt.Errorf("%w: unknown field %q in %s:%d", ErrSyntax, key, origin, row)
				}
				patterns, err := shellescape.Split(iu.String())
				if err != nil {
					return fmt.Errorf("can't split IgnoreUnknown directive %q in %s:%d: %w", iu.String(), origin, row, err)
				}
				match, err := matchesPatterns(patterns, key)
				if err != nil {
					return fmt.Errorf("match IgnoreUnknown directive %q in %s:%d: %w", iu.String(), origin, row, err)
				}
				if !match {
					return fmt.Errorf("%w: unknown field %q in %s:%d", ErrSyntax, key, origin, row)
				}
			}
		}
	}
	return nil
}

func (p *Parser) canonicalize(obj withRequiredFields) error {
	host, _ := obj.GetHost()
	co, ok := obj.(withCanonicalize)
	if !ok {
		return nil
	}
	newHost, useOld := co.Canonicalize(host)
	if newHost == "" {
		if !useOld {
			return fmt.Errorf("%w: hostname %q could not be canonicalized and fallback is disabled", ErrCanonicalizationFailed, host)
		}
	} else {
		// store the old => new to avoid doing the same canonicalization again and again in an infinite loop
		if p.canonicalized == nil {
			p.canonicalized = make(map[string]string)
		}

		if prevNh, ok := p.canonicalized[host]; ok && prevNh != newHost {
			p.canonicalized[host] = newHost
			_ = obj.SetHost(newHost)
			return errRedo
		}
	}
	return nil
}

type withRequiredFields interface {
	SetUser(username string) error
	SetHost(host string) error
	SetHostname(hostname string) error
	GetHost() (string, bool)
}

type withCanonicalize interface {
	Canonicalize(addr string) (string, bool)
}

// Parse the ssh config for the parts that apply to the passed in object.
// The object must have fields for user, host and hostname. The host
// field must be set before calling this function.
func (p *Parser) Parse(obj withRequiredFields) error { //nolint:cyclop,gocognit
	fields, err := objFields(obj)
	if err != nil {
		return fmt.Errorf("check configuration object compatibility: %w", err)
	}
	if err := p.parse(obj, fields, strings.NewReader(sshDefaultConfig), ValueOriginDefault, "", true); err != nil {
		if errors.Is(err, errRedo) {
			if err := p.Parse(obj); err != nil {
				return fmt.Errorf("second pass after canonicalization failed: %w", err)
			}
			return nil
		}
		// defaults do not contain canonicalization
		return fmt.Errorf("failed to parse default ssh config: %w", err)
	}

	// Reader is provided, parse it and nothing else
	if p.r != nil { //nolint:nestif
		if err := p.parse(obj, fields, p.r, ValueOriginFile, p.rname, true); err != nil {
			if errors.Is(err, errRedo) {
				if err := p.Parse(obj); err != nil {
					return fmt.Errorf("second pass after canonicalization failed: %w", err)
				}
				return nil
			}
			return fmt.Errorf("failed to parse ssh config: %w", err)
		}
		return nil
	}

	// No reader provided, parse user and global config
	if p.UserConfigPath == "" {
		if cfg, err := homedir.Expand("~/.ssh/config"); err == nil {
			p.UserConfigPath = cfg
		}
	}
	if p.UserConfigPath != "" { //nolint:nestif
		userConfigFile, err := os.Open(p.UserConfigPath)
		if err == nil {
			defer userConfigFile.Close()
			if err := p.parse(obj, fields, userConfigFile, ValueOriginFile, p.UserConfigPath, true); err != nil {
				if errors.Is(err, errRedo) {
					if err := p.Parse(obj); err != nil {
						return fmt.Errorf("second pass after canonicalization failed: %w", err)
					}
					return nil
				}
				return fmt.Errorf("parsing user config from %s failed: %w", p.UserConfigPath, err)
			}
		}
	}

	if p.GlobalConfigPath == "" {
		p.GlobalConfigPath = defaultGlobalConfigPath()
	}
	globalConfigFile, err := os.Open(p.GlobalConfigPath)
	if err == nil { //nolint:nestif
		defer globalConfigFile.Close()
		if err := p.parse(obj, fields, globalConfigFile, ValueOriginFile, p.GlobalConfigPath, true); err != nil {
			if errors.Is(err, errRedo) {
				if err := p.Parse(obj); err != nil {
					return fmt.Errorf("second pass after canonicalization failed: %w", err)
				}
				return nil
			}
			return fmt.Errorf("parsing global config from %s failed: %w", p.GlobalConfigPath, err)
		}
	}

	if !p.doingFinal && p.hasFinal {
		// second pass to handle Match final directives and other Matchblocks that
		// may match after hostname has been canonicalized.
		if p.r != nil {
			if _, err := p.r.Seek(0, 0); err != nil {
				return fmt.Errorf("error seeking to the start of the reader: %w", err)
			}
		}
		p.doingFinal = true
		if err := p.Parse(obj); err != nil {
			return fmt.Errorf("final pass of parsing ssh config failed: %w", err)
		}
	}

	return nil
}

// Reset resets the parser to its initial state.
func (p *Parser) Reset() {
	if p.r != nil {
		_, _ = p.r.Seek(0, 0)
	}
	p.originalHost = ""
	p.didCanonicalize = false
	p.hasFinal = false
	p.doingFinal = false
}

func expand(input string) (string, error) {
	val, err := shellescape.Expand(input, shellescape.ExpandNoDollarVars(), shellescape.ExpandErrorIfUnset())
	if err != nil {
		return "", fmt.Errorf("expand: %w", err)
	}
	return val, nil
}
