// Package sshconfig provides a parser for OpenSSH ssh_config files as
// documented in the [man page].
//
// # Implemented features:
//
//   - Go mappings for all of the keys known to OpenSSH client (and two
//     additional Apple specific fields from the Mac port).
//   - Partial Match directive support. Address, LocalAddress, LocalPort and
//     RDomain are not implemented because they require passing
//     in a connection, which is not (yet?) implemented.
//   - Partial token expansion support. Like above, expanding some of the
//     tokens would require an established connection.
//   - Include directive support, the parser will follow the Include directives
//     in lexical order as specified in the spec.
//   - Expansion of ~ and environment variables in the values where applicable
//     (the enabled fields are listed in the [man page]).
//   - Support for list modifier prefixes for fields like HostKeyAlgorithms or
//     KexAlgorithms where you can use "+" prefix to append to default list,
//     "-" to remove from the default list and ^ to prepend to the default
//     list.
//   - Support for boolean fields which can also have string values (yes, no,
//     ask, always, none, etc.). These are not enforced or validated like in
//     the OpenSSH implementation, if the field is a MultiStateBooleanValue,
//     it will accept any string value, but Bool() will return the boolean
//     value and an ok flag indicating if the value is one of the known boolean
//     values.
//   - "Strict" mode for supporting the IgnoreUnknown directive. When enabled,
//     the parser will throw an error when it encounters an unknown directive.
//     By default this is not enabled. To enable, use the
//     [WithErrorOnUnknown] option when creating the parser.
//   - The origin of each value can be determined.
//   - The order based value precedence is correctly implemented as described
//     in the specification.
//   - Hostname canonicalization.
//   - Original-like unquoting and splitting of values based on `argv_split`
//     from the OpenSSH source converted to go.
//
// # Usage:
//
// What you create a new [Parser], what you get is not a key-value store that
// you can use to query values for keys. Upon initialization, the Parser
// reads in the ssh configuration files and creates an internal tree structure
// that will be iterated over for each host configuration object to be
// populated with values from the configuration.
//
// The SSH configuration files do not define a list of hosts, it's not a
// phone-book. The configuration is a set of rules that match hostnames or
// other attributes like the username or the current local address and
// the settings are applied only when they match the current connection.
//
// You can either use the full [Config] which includes all the known keys
// or you can define a struct with a subset of the keys that you are
// interested in.
//
// [man page]: https://man7.org/linux/man-pages/man5/ssh_config.5.html
package sshconfig

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"sync"

	"github.com/k0sproject/rig/v2/log"
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

// Exported errors.
var (
	// ErrSyntax is returned when the config file has a syntax error.
	ErrSyntax = errors.New("syntax error")

	// ErrInvalidObject is returned when the object passed to Apply is not compatible.
	ErrInvalidObject = errors.New("invalid object")

	// ErrNotImplemented is returned when encountering a ssh_config feature that has not been
	// implemented.
	ErrNotImplemented = errors.New("not implemented")

	username = sync.OnceValue(
		func() string {
			if user, err := user.Current(); err == nil {
				return user.Username
			}
			return ""
		},
	)

	userhome = sync.OnceValue(
		func() string {
			if home, err := os.UserHomeDir(); err == nil {
				return home
			}
			return ""
		},
	)
)

// the word "host" is used so many times it's worth making a constant for it.
const fkHost = "host"

// Parser for OpenSSH ssh_config files.
type Parser struct {
	mu sync.Mutex

	iter *treeIterator

	options parserOptions
}

// parserOptions for the ssh config parser.
type parserOptions struct {
	errorOnUnknown     bool
	globalConfigPath   string
	userConfigPath     string
	globalConfigReader io.Reader
	userConfigReader   io.Reader
	executor           executor
	nofinalize         bool
	home               string
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
type ParserOption func(*parserOptions)

// newParserOptions returns a new ParserOptions with the given options applied.
func newParserOptions(opts ...ParserOption) parserOptions {
	options := parserOptions{executor: defaultExecutor{}}
	for _, opt := range opts {
		opt(&options)
	}
	return options
}

// WithStrict is a functional option that makes the parser respect the 'IgnoreUnknown'
// directive, thus making it error out on any encountered key that is not found and is not listed
// in the "IgnoreUnknown" config field.
func WithStrict() ParserOption {
	return func(o *parserOptions) {
		o.errorOnUnknown = true
	}
}

// WithNoFinalize is a functional option that disables the finalization of the object.
func WithNoFinalize() ParserOption {
	return func(o *parserOptions) {
		o.nofinalize = true
	}
}

// WithGlobalConfigPath is a functional option that overrides the default global config path
// (/etc/ssh/ssh_config or %PROGRAMDATA%/ssh/ssh_config on Windows).
func WithGlobalConfigPath(path string) ParserOption {
	return func(o *parserOptions) {
		o.globalConfigPath = path
	}
}

// WithUserConfigPath is a functional option that overrides the default user config path (~/.ssh/config).
func WithUserConfigPath(path string) ParserOption {
	return func(o *parserOptions) {
		o.userConfigPath = path
	}
}

// WithGlobalConfigReader is a functional option that overrides the default global config reader.
func WithGlobalConfigReader(r io.Reader) ParserOption {
	return func(o *parserOptions) {
		o.globalConfigReader = r
	}
}

// WithUserConfigReader is a functional option that overrides the default user config reader.
func WithUserConfigReader(r io.Reader) ParserOption {
	return func(o *parserOptions) {
		o.userConfigReader = r
	}
}

// WithExecutor is a functional option that overrides the default executor for testing (or disabling?) purposes.
func WithExecutor(e executor) ParserOption {
	return func(o *parserOptions) {
		o.executor = e
	}
}

// WithUserHome is a functional option that sets the home directory for the current user.
func WithUserHome(home string) ParserOption {
	return func(o *parserOptions) {
		o.home = home
	}
}

// NewParser returns a new Parser. If r is nil, the default ssh config files will be read
// from ~/.ssh/config and /etc/ssh/ssh_config (or %PROGRAMDATA%/ssh/ssh_config on Windows).
//
// Calling NewParser will perform the initial parsing of the configuration files and returns
// an error if it fails.
//
// If your use-case is to parse the configuration for multiple hosts, you only need to
// create one parser and use it to apply settings to multiple host objects by calling
// Apply multiple times with different objects..
func NewParser(r io.Reader, opts ...ParserOption) (*Parser, error) {
	options := newParserOptions(opts...)
	treeParser := newTreeParser(r)
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
	return &Parser{iter: iter, options: options}, nil
}

// ConfigFor returns a new Config for the given host.
//
// This is a shorthand for creating a [Parser], and using it to populate a [Config] object.
//
// Do not use this if you need to get configurations for multiple hosts.
func ConfigFor(host string, opts ...ParserOption) (*Config, error) {
	parser, err := NewParser(nil, opts...)
	if err != nil {
		return nil, fmt.Errorf("create parser: %w", err)
	}
	config := &Config{}
	if err := parser.Apply(config, host); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return config, nil
}

func (p *Parser) apply(setter *Setter) error {
	for p.iter.Next() {
		key := p.iter.Key()
		values := p.iter.Values()
		path := p.iter.Path()
		row := p.iter.Row()

		setter.applyingDefaults(path == "__default__")

		switch key {
		case "include":
			// just keep on iterating, the tree parser has already included the file as just another beanch
			continue
		case fkHost:
			match, err := setter.matchesHost(values...)
			if err != nil {
				return err
			}
			if !match {
				// Not for this host, skip the block.
				p.iter.Skip()
			}
		case "match":
			match, err := setter.matchesMatch(values...)
			if err != nil {
				return fmt.Errorf("can't process Match directive %q in %s:%d: %w", values, path, row, err)
			}
			log.Trace(context.Background(), "match directive result", "match", match, "values", values, "path", path, "row", row)
			if !match {
				// Not for this host, skip the block.
				p.iter.Skip()
			}
		default:
			if err := setter.Set(key, values...); err != nil {
				return fmt.Errorf("set %q: %w", key, err)
			}
		}
	}
	return nil
}

// Apply the ssh config into the passed in object, such as an instance
// of [Config]. The object needs at least a Host (string) field.
func (p *Parser) Apply(obj any, host string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.reset()

	setter, err := NewSetter(obj)
	if err != nil {
		return fmt.Errorf("create setter: %w", err)
	}

	if p.options.errorOnUnknown {
		setter.ErrorOnUnknownFields = true
	}

	if p.options.home != "" {
		setter.home = p.options.home
	}

	setter.executor = p.options.executor

	if err := setter.Set(fkHost, host); err != nil {
		return fmt.Errorf("set host %q: %w", host, err)
	}

	p.iter.Reset()
	if err := p.apply(setter); err != nil {
		return fmt.Errorf("failed to apply ssh config: %w", err)
	}

	if err := setter.CanonicalizeHostname(); err != nil {
		return fmt.Errorf("canonicalize hostname: %w", err)
	}

	if setter.wantFinal || setter.HostChanged() {
		setter.doingFinal()
		p.iter.Reset()
		if err := p.apply(setter); err != nil {
			return fmt.Errorf("second pass of ssh config application failed: %w", err)
		}
	}

	if !p.options.nofinalize {
		if err := setter.Finalize(); err != nil {
			return fmt.Errorf("finalize: %w", err)
		}
	}

	return nil
}

func (p *Parser) reset() {
	p.iter.Reset()
}

// Reset resets the parser to its initial state.
func (p *Parser) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reset()
}
