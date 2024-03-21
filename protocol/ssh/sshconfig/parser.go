// Package sshconfig provides a parser for openssh ssh_config files as documented in https://man7.org/linux/man-pages/man5/ssh_config.5.html manual pages.
package sshconfig

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/user"
	"reflect"
	"regexp"
	"strings"
	"sync"

	"github.com/k0sproject/rig/v2/log"
	"github.com/k0sproject/rig/v2/protocol/ssh/sshconfig/tree"
	"github.com/k0sproject/rig/v2/protocol/ssh/sshconfig/value"
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

	// ErrCanonicalizationFailed is returned when a hostname could not be canonicalized.
	ErrCanonicalizationFailed = errors.New("canonicalization failed")

	// ErrNotImplemented is returned when encountering a feature that is not implemented.
	ErrNotImplemented = errors.New("not implemented")

	// ErrSyntax is returned when the config file has a syntax error.
	ErrSyntax = tree.ErrSyntax

	// if the host alias gets canonicalized, the parser needs to start over.
	errRedo = errors.New("redo after canonicalization")

	username = sync.OnceValue(
		func() string {
			if user, err := user.Current(); err == nil {
				return user.Username
			}
			return ""
		},
	)
)

const fkHost = "host"

// Parser is a parser for openssh ssh_config files.
type Parser struct {
	mu sync.Mutex

	errorOnUnknown  bool
	iter            *tree.TreeIterator
	originalHost    string
	rname           string
	canonicalized   map[string]string
	didCanonicalize bool // if the host alias was canonicalized
	hasFinal        bool // if the config has a final directive
	doingFinal      bool // if the parser is currently parsing a final directive

	executor executor
}

// ParserOptions for the ssh config parser.
type ParserOptions struct {
	errorOnUnknown     bool
	globalConfigPath   string
	userConfigPath     string
	globalConfigReader io.Reader
	userConfigReader   io.Reader
	executor           executor
}

type executor interface {
	Run(cmd string, args ...string) error
}

type defaultExecutor struct{}

func (d defaultExecutor) Run(cmd string, args ...string) error {
	if err := exec.Command(cmd, args...).Run(); err != nil {
		return fmt.Errorf("run command %q: %w", cmd, err)
	}
	return nil
}

// ParserOption is a function that sets a parser option.
type ParserOption func(*ParserOptions)

// NewParserOptions returns a new ParserOptions with the given options applied.
func NewParserOptions(opts ...ParserOption) *ParserOptions {
	options := &ParserOptions{executor: defaultExecutor{}}
	for _, opt := range opts {
		opt(options)
	}
	return options
}

// WithErrorOnUnknown is a functional option that makes the parser respect the 'IgnoreUnknown' directive,
// thus meaning it will error out unless any encountered unknown key does not match any of the patterns
// in the 'IgnoreUnknown' field of the object.
func WithErrorOnUnknown() ParserOption {
	return func(o *ParserOptions) {
		o.errorOnUnknown = true
	}
}

// WithGlobalConfigPath is a functional option that overrides the default global config path (/etc/ssh/ssh_config
// or PROGRAMDATA/ssh/ssh_config on windows).
func WithGlobalConfigPath(path string) ParserOption {
	return func(o *ParserOptions) {
		o.globalConfigPath = path
	}
}

// WithUserConfigPath is a functional option that overrides the default user config path (~/.ssh/config).
func WithUserConfigPath(path string) ParserOption {
	return func(o *ParserOptions) {
		o.userConfigPath = path
	}
}

// WithGlobalConfigReader is a functional option that overrides the default global config reader.
func WithGlobalConfigReader(r io.Reader) ParserOption {
	return func(o *ParserOptions) {
		o.globalConfigReader = r
	}
}

// WithUserConfigReader is a functional option that overrides the default user config reader.
func WithUserConfigReader(r io.Reader) ParserOption {
	return func(o *ParserOptions) {
		o.userConfigReader = r
	}
}

// WithExecutor is a functional option that overrides the default executor for testing (or disabling?) purposes.
func WithExecutor(e executor) ParserOption {
	return func(o *ParserOptions) {
		o.executor = e
	}
}

// HostConfig interface defines the methods that the object passed to Parse must implement. The
// easiest way to implement this is to embed `sshconfig.RequiredFields` into a struct.
//
// Example:
//
//	type MyConfig struct {
//	    sshconfig.RequiredFields
//	    IdentityFile sshconfig.PathListValue
//	 }
type HostConfig interface {
	SetUser(username string) error
	SetHost(host string) error
	SetHostname(hostname string) error
	GetHost() (string, bool)
}

type withCanonicalize interface {
	Canonicalize(addr string) (string, bool)
}

// NewParser returns a new Parser that uses a [TreeIterator] to walk the ssh config tree.
func NewParser(r io.Reader, opts ...ParserOption) (*Parser, error) {
	options := NewParserOptions(opts...)
	treeParser := tree.NewTreeParser(r)
	if options.globalConfigPath != "" {
		treeParser.GlobalConfigPath = options.globalConfigPath
	}
	if options.userConfigPath != "" {
		treeParser.UserConfigPath = options.userConfigPath
	}
	if options.globalConfigReader != nil {
		treeParser.GlobalConfigReader = options.globalConfigReader
	}
	if options.userConfigReader != nil {
		treeParser.UserConfigReader = options.userConfigReader
	}
	iter, err := treeParser.Parse()
	if err != nil {
		return nil, fmt.Errorf("failed to tokenize ssh config: %w", err)
	}
	return &Parser{iter: iter, errorOnUnknown: options.errorOnUnknown, executor: options.executor}, nil
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
		if h, err := os.Hostname(); err == nil {
			return h
		}
	case "%h":
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
	case "%L":
		if h, err := os.Hostname(); err == nil {
			return h
		}
	}
	// unsupported or unknown token
	return token
}

type configValue interface {
	SetString(val string, origin string) error
	SetStrings(vals []string, origin string) error
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

	if _, ok := obj.(HostConfig); !ok {
		return nil, fmt.Errorf("%w: object does not contain the required fields", ErrInvalidObject)
	}

	return fields, nil
}

// matchesMatch parses the Match directive's value and determines if it applies to the current context.
func (p *Parser) matchesMatch(conditions []string, obj HostConfig, fields map[string]configValue) (bool, error) { //nolint:gocognit,cyclop,funlen,gocyclo // TODO split this up
	for i := 0; i < len(conditions); i++ {
		statement := strings.ToLower(conditions[i])

		if statement == "all" {
			if len(conditions) > 1 {
				return false, fmt.Errorf("%w: match condition %q must be alone", ErrSyntax, statement)
			}
			return true, nil
		}

		if statement == "canonical" {
			if !p.doingFinal {
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
		conditionValue := conditions[i]

		switch statement {
		case "user":
			userValue, ok := fields["user"].(*value.StringValue)
			if !ok {
				return false, fmt.Errorf("%w: user field not found or is not a StringValue", ErrInvalidObject)
			}
			user, ok := userValue.Get()
			if !ok {
				return false, fmt.Errorf("%w: user not set for evaluating match condition", ErrInvalidObject)
			}
			conditionValues := strings.Split(conditionValue, ",")
			match, err := value.MatchAll(user, conditionValues...)
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
			conditionValues := strings.Split(conditionValue, ",")
			match, err := value.MatchAll(p.originalHost, conditionValues...)
			if err != nil {
				return false, fmt.Errorf("match originalhost for match condition: %w", err)
			}
			if !match {
				return false, nil
			}
		case fkHost:
			host, ok := obj.GetHost()
			if !ok {
				return false, fmt.Errorf("%w: host field not set", ErrInvalidObject)
			}
			conditionValues := strings.Split(conditionValue, ",")
			match, err := value.MatchAll(host, conditionValues...)
			if err != nil {
				return false, fmt.Errorf("match host for match condition: %w", err)
			}
			if !match {
				return false, nil
			}
		case "exec":
			parts, err := shellescape.Split(conditionValue)
			if err != nil {
				return false, fmt.Errorf("split exec command: %w", err)
			}
			if err := p.executor.Run(parts[0], parts[1:]...); err != nil {
				return false, nil //nolint:nilerr // error content is not relevant
			}
		case "group":
			user, err := user.Current()
			if err != nil {
				return false, fmt.Errorf("can't get current user: %w", err)
			}
			groups, err := user.GroupIds()
			if err != nil {
				return false, fmt.Errorf("can't get current user groups: %w", err)
			}
			conditionValues := strings.Split(conditionValue, ",")
			for _, group := range groups {
				match, err := value.MatchAll(group, conditionValues...)
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
			if !hostTagValue.IsSet() {
				return false, nil
			}
			conditionValues := strings.Split(conditionValue, ",")
			match, err := value.MatchAll(hostTagValue.String(), conditionValues...)
			if err != nil {
				return false, fmt.Errorf("match tag for match condition: %w", err)
			}
			if !match {
				return false, nil
			}
		case "address", "localaddress", "localport", "rdomain":
			return false, fmt.Errorf("%w: match condition %q not supported by the ssh config parser", ErrNotImplemented, statement)
		}
	}
	return true, nil
}

var tokenRe = regexp.MustCompile(`%[a-zA-Z%]`)

type withAttrs interface {
	log.TraceLogger
	With(attrs ...any) *slog.Logger
}

func (p *Parser) parse(obj HostConfig, fields map[string]configValue) error { //nolint:cyclop,gocognit,funlen,gocyclo,maintidx // TODO a pretty strong hint to split this up
	host, ok := obj.GetHost()
	if !ok {
		return fmt.Errorf("%w: host field is not set", ErrInvalidObject)
	}

	ctx := context.Background()

	var trace log.TraceLogger
	tlog := log.GetTraceLogger()
	for p.iter.Next() {
		key := p.iter.Key()
		values := p.iter.Values()
		path := p.iter.Path()
		row := p.iter.Row()
		if tl, ok := tlog.(withAttrs); ok {
			trace = tl.With("origin", path, "row", row, "hostalias", host, "key", key)
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
		case "challengeresponseauthentication":
			// deprecated alias
			key = "kbdinteractiveauthentication"
		case "hostbasedkeytypes":
			// deprecated alias
			key = "hostbasedacceptedalgorithms"
		case "pubkeyacceptedkeytypes":
			// deprecated alias
			key = "pubkeyacceptedalgorithms"
		case "certificatefile", "controlpath", "identityagent", "identityfile", "knownhostscommand", "userknownhostsfile":
			for i, value := range values {
				val, err := expand(value)
				if err != nil {
					return fmt.Errorf("expand value %q for %q in %s:%d: %w", value, key, path, row, err)
				}
				values[i] = val
			}
		case "localforward", "remoteforward":
			// these fields only accept a socket path from expansion
			for i, value := range values {
				val, err := expand(value)
				if err != nil {
					return fmt.Errorf("expand value %q for %q in %s:%d: %w", value, key, path, row, err)
				}
				unq, err := shellescape.Unquote(val)
				if err != nil {
					return fmt.Errorf("unquote value %q for %q in %s:%d: %w", val, key, path, row, err)
				}
				if _, err := os.Stat(unq); err != nil {
					return fmt.Errorf("stat value %q for %q in %s:%d: %w", unq, key, path, row, err)
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
					return fmt.Errorf("canonicalize host %q in %s:%d: %w", host, path, row, err)
				}
			}
			match, err := value.MatchAll(host, values...)
			if err != nil {
				return err
			}
			trace.Log(ctx, slog.LevelInfo, "evaluated host block conditions", "conditions", values, "applies", match)
			if !match {
				p.iter.Skip()
			}
		case "include":
			hostBefore, _ := obj.GetHost()
			if err := p.parse(obj, fields); err != nil {
				return fmt.Errorf("failed to parse include file %s in %s:%d: %w", values[0], path, row, err)
			}
			hostAfter, _ := obj.GetHost()
			if hostBefore != hostAfter {
				trace.Log(ctx, slog.LevelInfo, "host alias changed during Include, triggering a redo", "before", hostBefore, "after", hostAfter)
				return errRedo
			}
		case "match":
			if p.didCanonicalize {
				trace.Log(ctx, slog.LevelInfo, "canonicalization was done during the last block before Match")
				p.didCanonicalize = false
				if err := p.canonicalize(obj); err != nil {
					return fmt.Errorf("canonicalize host %q in %s:%d: %w", host, path, row, err)
				}
			}
			if values[0] == "all" {
				continue
			}

			matches, err := p.matchesMatch(values, obj, fields)
			if err != nil {
				return fmt.Errorf("can't parse Match directive %q in %s:%d: %w", values, path, row, err)
			}
			trace.Log(ctx, slog.LevelInfo, "evaluated match conditions", "conditions", values, "applies", matches)
			if !matches {
				p.iter.Skip()
			}
		case "proxyjump":
			// proxyjump and proxycommand are mutually exclusive, the first one to be set wins.
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

			if err := pjump.SetStrings([]string{shellescape.QuoteCommand(values)}, path); err != nil {
				return fmt.Errorf("set value %q for proxycommand in %s:%d: %w", values, path, row, err)
			}
		case "proxycommand":
			// proxyjump and proxycommand are mutually exclusive, the first one to be set wins.
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

			if err := pcmd.SetStrings([]string{shellescape.QuoteCommand(values)}, path); err != nil {
				return fmt.Errorf("set value %q for proxycommand in %s:%d: %w", values, path, row, err)
			}
		default:
			if _, ok := canonicalizationFields[key]; ok {
				trace.Log(ctx, slog.LevelInfo, "touched canonicalization field")
				p.didCanonicalize = true
			}

			if f, ok := fields[key]; ok { //nolint:nestif
				trace.Log(ctx, slog.LevelInfo, "setting value", "value", values)
				if err := f.SetStrings(values, path); err != nil {
					return fmt.Errorf("set value %q for %q in %s:%d: %w", values, key, path, row, err)
				}
			} else if p.errorOnUnknown {
				iu, ok := fields["ignoreunknown"]
				if !ok || !iu.IsSet() || iu.String() == "" {
					return fmt.Errorf("%w: unknown field %q in %s:%d", ErrSyntax, key, path, row)
				}
				patterns, err := shellescape.Split(iu.String())
				if err != nil {
					return fmt.Errorf("can't split IgnoreUnknown directive %q in %s:%d: %w", iu.String(), path, row, err)
				}
				match, err := value.MatchAll(key, patterns...)
				if err != nil {
					return fmt.Errorf("match IgnoreUnknown directive %q in %s:%d: %w", iu.String(), path, row, err)
				}
				if !match {
					return fmt.Errorf("%w: unknown field %q in %s:%d", ErrSyntax, key, path, row)
				}
			}
		}
	}
	return nil
}

func (p *Parser) canonicalize(obj HostConfig) error {
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

// Parse the ssh config for the parts that apply to the passed in object.
// The object must have fields for user, host and hostname. The host
// field must be set before calling this function.
func (p *Parser) Parse(obj HostConfig, host string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.reset()

	if host == "" {
		h, ok := obj.GetHost()
		if !ok {
			return fmt.Errorf("%w: host field is not set", ErrInvalidObject)
		}
		host = h
	} else {
		_ = obj.SetHost(host)
	}
	p.originalHost = host
	fields, err := objFields(obj)
	if err != nil {
		return fmt.Errorf("check configuration object compatibility: %w", err)
	}
	p.iter.Reset()
	if err := p.parse(obj, fields); err != nil {
		if !errors.Is(err, errRedo) {
			return fmt.Errorf("failed to parse ssh config: %w", err)
		}
	}

	if p.hasFinal || p.didCanonicalize {
		p.doingFinal = true
		p.iter.Reset()
		if err := p.parse(obj, fields); err != nil {
			return fmt.Errorf("second pass of ssh config parsing failed: %w", err)
		}
	}

	return nil
}

func (p *Parser) reset() {
	p.iter.Reset()
	p.originalHost = ""
	p.didCanonicalize = false
	p.hasFinal = false
	p.doingFinal = false
}

// Reset resets the parser to its initial state.
func (p *Parser) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reset()
}

func expand(input string) (string, error) {
	val, err := shellescape.Expand(input, shellescape.ExpandNoDollarVars(), shellescape.ExpandErrorIfUnset())
	if err != nil {
		return "", fmt.Errorf("expand: %w", err)
	}
	return val, nil
}
