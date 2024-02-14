package rig

import (
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/initsystem"
	"github.com/k0sproject/rig/log"
	"github.com/k0sproject/rig/packagemanager"
	"github.com/k0sproject/rig/remotefs"
)

// Options is a struct that holds the variadic options for the rig package.
type Options struct {
	connectionDependencies *Dependencies
}

// Apply applies the supplied options to the Options struct.
func (o *Options) Apply(opts ...Option) {
	for _, opt := range opts {
		opt(o)
	}
}

// ConnectionDependencies returns configured dependencies for a connection.
func (o *Options) ConnectionDependencies() *Dependencies {
	return o.connectionDependencies
}

// Option is a functional option type for the Options struct.
type Option func(*Options)

// WithClient is a functional option that sets the client to use for connecting instead of getting it from the ClientConfigurer.
func WithClient(client Connection) Option {
	return func(o *Options) {
		o.connectionDependencies.client = client
	}
}

// WithRunner is a functional option that sets the runner to use for executing commands.
func WithRunner(runner exec.Runner) Option {
	return func(o *Options) {
		o.connectionDependencies.Runner = runner
	}
}

// WithLogger is a functional option that sets the logger to use for logging.
func WithLogger(logger log.Logger) Option {
	return func(o *Options) {
		o.connectionDependencies.SetLogger(logger)
	}
}

// WithProtocolConfigurer is a functional option that sets the client configurer to use for connecting.
func WithProtocolConfigurer(configurer ProtocolConfigurer) Option {
	return func(o *Options) {
		o.connectionDependencies.protocolConfigurer = configurer
	}
}

// WithProviders is a functional option that sets the repositories to use for the connection.
func WithProviders(providers SubsystemProviders) Option {
	return func(o *Options) {
		o.connectionDependencies.providers = providers
	}
}

// WithFS is a functional option that sets the filesystem to use for the connection.
func WithFS(fs remotefs.FS) Option {
	return func(o *Options) {
		o.connectionDependencies.fs = fs
	}
}

// WithPackageManager is a functional option that sets the package manager to use for the connection.
func WithPackageManager(packagemanager packagemanager.PackageManager) Option {
	return func(o *Options) {
		o.connectionDependencies.packageMan = packagemanager
	}
}

// WithInitSystem is a functional option that sets the init system to use for the connection.
func WithInitSystem(initsystem initsystem.ServiceManager) Option {
	return func(o *Options) {
		o.connectionDependencies.initSys = initsystem
	}
}

// WithInitSystemProvider is a functional option that sets the init system repository to use for the connection.
func WithInitSystemProvider(initsysProvider initsystemProvider) Option {
	return func(o *Options) {
		o.connectionDependencies.providers.initsys = initsysProvider
	}
}

// WithPackageManagerProvider is a functional option that sets the package manager repository to use for the connection.
func WithPackageManagerProvider(packagemanProvider packagemanagerProvider) Option {
	return func(o *Options) {
		o.connectionDependencies.providers.packagemanager = packagemanProvider
	}
}

// WithSudoProvider is a functional option that sets the sudo repository to use for the connection.
func WithSudoProvider(sudoProvider sudoProvider) Option {
	return func(o *Options) {
		o.connectionDependencies.providers.sudo = sudoProvider
	}
}

// WithRemoteFSRepository is a functional option that sets the filesystem repository to use for the connection.
func WithRemoteFSRepository(fsProvider fsProvider) Option {
	return func(o *Options) {
		o.connectionDependencies.providers.fs = fsProvider
	}
}

// WithLoggerFactory is a functional option that sets the logger factory to use for creating a logger for the connection.
func WithLoggerFactory(loggerFactory LoggerFactory) Option {
	return func(o *Options) {
		o.connectionDependencies.providers.loggerFactory = loggerFactory
	}
}

// NewOptions creates a new Options struct with the supplied options applied over the defaults.
func NewOptions(opts ...Option) *Options {
	options := &Options{
		connectionDependencies: DefaultDependencies(),
	}

	options.Apply(opts...)

	return options
}
