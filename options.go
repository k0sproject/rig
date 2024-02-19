package rig

import (
	"fmt"

	"github.com/k0sproject/rig/abort"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/initsystem"
	"github.com/k0sproject/rig/log"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/packagemanager"
	"github.com/k0sproject/rig/protocol"
	"github.com/k0sproject/rig/remotefs"
	"github.com/k0sproject/rig/sudo"
)

// LoggerFactory is a function that creates a logger.
type LoggerFactory func(protocol.Connection) log.Logger

var nullLogger = &log.NullLog{}

// defaultLoggerFactory returns a logger factory that returns a null logger.
func defaultLoggerFactory(_ protocol.Connection) log.Logger {
	return nullLogger
}

// ConnectionConfigurer can create connections. When a connection is not given, the configurer is used
// to build a connection.
type ConnectionConfigurer interface {
	fmt.Stringer
	Connection() (protocol.Connection, error)
}

func defaultConnectionConfigurer() ConnectionConfigurer {
	return &CompositeConfig{}
}

// Options is a struct that holds the variadic options for the rig package.
type Options struct {
	connection           protocol.Connection
	connectionConfigurer ConnectionConfigurer
	loggerFactory        LoggerFactory
	runner               exec.Runner
	providers
}

type providers struct {
	packageManagerProvider
	initSystemProvider
	remoteFSProvider
	osReleaseProvider
	sudoProvider
}

type packageManagerProvider struct {
	provider packagemanager.PackageManagerProvider
}

func (p *packageManagerProvider) GetPackageManagerService(runner exec.Runner) *packagemanager.Service {
	return packagemanager.NewPackageManagerService(p.provider, runner)
}

type initSystemProvider struct {
	provider initsystem.InitSystemProvider
}

func (p *initSystemProvider) GetInitSystemService(runner exec.Runner) *initsystem.Service {
	return initsystem.NewInitSystemService(p.provider, runner)
}

type remoteFSProvider struct {
	provider remotefs.RemoteFSProvider
}

func (p *remoteFSProvider) GetRemoteFSService(runner exec.Runner) *remotefs.Service {
	return remotefs.NewRemoteFSService(p.provider, runner)
}

type osReleaseProvider struct {
	provider os.OSReleaseProvider
}

func (p *osReleaseProvider) GetOSReleaseService(runner exec.Runner) *os.Service {
	return os.NewOSReleaseService(p.provider, runner)
}

type sudoProvider struct {
	provider sudo.SudoProvider
}

func (p *sudoProvider) GetSudoService(runner exec.Runner) *sudo.Service {
	return sudo.NewSudoService(p.provider, runner)
}

func defaultProviders() providers {
	return providers{
		packageManagerProvider: packageManagerProvider{provider: packagemanager.DefaultProvider()},
		initSystemProvider:     initSystemProvider{provider: initsystem.DefaultProvider()},
		remoteFSProvider:       remoteFSProvider{provider: remotefs.DefaultProvider()},
		osReleaseProvider:      osReleaseProvider{provider: os.DefaultProvider()},
		sudoProvider:           sudoProvider{provider: sudo.DefaultProvider()},
	}
}

// Apply applies the supplied options to the Options struct.
func (o *Options) Apply(opts ...Option) {
	for _, opt := range opts {
		opt(o)
	}
}

func (o *Options) Clone() *Options {
	return &Options{
		connection:           o.connection,
		connectionConfigurer: o.connectionConfigurer,
		loggerFactory:        o.loggerFactory,
		runner:               o.runner,
		providers:            o.providers,
	}
}

func (o *Options) GetConnection() (protocol.Connection, error) {
	if o.connection != nil {
		return o.connection, nil
	}
	if o.connectionConfigurer == nil {
		return nil, fmt.Errorf("%w: no connection or connection configurer provided", abort.ErrAbort)
	}
	conn, err := o.connectionConfigurer.Connection()
	if err != nil {
		return nil, fmt.Errorf("create connection: %w", err)
	}
	return conn, nil
}

func (o *Options) GetRunner(conn protocol.Connection) exec.Runner {
	if o.runner != nil {
		return o.runner
	}
	return exec.NewHostRunner(conn)
}

// Option is a functional option type for the Options struct.
type Option func(*Options)

// WithConnection is a functional option that sets the client to use for connecting instead of getting it from the ConnectionConfigurer.
func WithConnection(conn protocol.Connection) Option {
	return func(o *Options) {
		o.connection = conn
	}
}

// WithRunner is a functional option that sets the runner to use for executing commands.
func WithRunner(runner exec.Runner) Option {
	return func(o *Options) {
		o.runner = runner
	}
}

// WithConnectionConfigurer is a functional option that sets the client configurer to use for connecting.
func WithConnectionConfigurer(configurer ConnectionConfigurer) Option {
	return func(o *Options) {
		o.connectionConfigurer = configurer
	}
}

// WithFSService is a functional option that sets the filesystem to use for the connection.
func WithRemoteFSProvider(provider remotefs.RemoteFSProvider) Option {
	return func(o *Options) {
		o.providers.remoteFSProvider = remoteFSProvider{provider: provider}
	}
}

// WithInitSystem is a functional option that sets the init system to use for the connection.
func WithInitSystemProvider(provider initsystem.InitSystemProvider) Option {
	return func(o *Options) {
		o.providers.initSystemProvider = initSystemProvider{provider: provider}
	}
}

func WithOSReleaseProvider(provider os.OSReleaseProvider) Option {
	return func(o *Options) {
		o.providers.osReleaseProvider = osReleaseProvider{provider: provider}
	}
}

// WithPackageManagerProvider is a functional option that sets the package manager repository to use for the connection.
func WithPackageManagerProvider(provider packagemanager.PackageManagerProvider) Option {
	return func(o *Options) {
		o.providers.packageManagerProvider = packageManagerProvider{provider: provider}
	}
}

func WithSudoProvider(provider sudo.SudoProvider) Option {
	return func(o *Options) {
		o.providers.sudoProvider = sudoProvider{provider: provider}
	}
}

// WithLoggerFactory is a functional option that sets the logger factory to use for creating a logger for the connection.
func WithLoggerFactory(loggerFactory LoggerFactory) Option {
	return func(o *Options) {
		o.loggerFactory = loggerFactory
	}
}

func DefaultOptions() *Options {
	return &Options{
		connectionConfigurer: &CompositeConfig{},
		loggerFactory:        defaultLoggerFactory,
		providers:            defaultProviders(),
	}
}

// NewOptions creates a new Options struct with the supplied options applied over the defaults.
func NewOptions(opts ...Option) *Options {
	options := DefaultOptions()
	options.Apply(opts...)
	return options
}
