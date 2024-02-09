package rig

import (
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/initsystem"
	"github.com/k0sproject/rig/log"
	"github.com/k0sproject/rig/packagemanager"
	"github.com/k0sproject/rig/rigfs"
)

// Options is a struct that holds the variadic options for the rig package.
type Options struct {
	*ConnectionInjectables
}

// Apply applies the supplied options to the Options struct.
func (o *Options) Apply(opts ...Option) {
	for _, opt := range opts {
		opt(o)
	}
}

// Option is a functional option type for the Options struct.
type Option func(*Options)

// WithClient is a functional option that sets the client to use for connecting instead of getting it from the ClientConfigurer.
func WithClient(client Client) Option {
	return func(o *Options) {
		o.client = client
	}
}

// WithRunner is a functional option that sets the runner to use for executing commands.
func WithRunner(runner exec.Runner) Option {
	return func(o *Options) {
		o.ConnectionInjectables.Runner = runner
	}
}

// WithLogger is a functional option that sets the logger to use for logging.
func WithLogger(logger log.Logger) Option {
	return func(o *Options) {
		o.ConnectionInjectables.SetLogger(logger)
	}
}

// WithClientConfigurer is a functional option that sets the client configurer to use for connecting.
func WithClientConfigurer(configurer ClientConfigurer) Option {
	return func(o *Options) {
		o.ConnectionInjectables.clientConfigurer = configurer
	}
}

// WithRepositories is a functional option that sets the repositories to use for the connection.
func WithRepositories(repositories ConnectionRepositories) Option {
	return func(o *Options) {
		o.ConnectionInjectables.repositories = repositories
	}
}

// WithFsys is a functional option that sets the filesystem to use for the connection.
func WithFsys(fsys rigfs.Fsys) Option {
	return func(o *Options) {
		o.ConnectionInjectables.fsys = fsys
	}
}

// WithPackageManager is a functional option that sets the package manager to use for the connection.
func WithPackageManager(packagemanager packagemanager.PackageManager) Option {
	return func(o *Options) {
		o.ConnectionInjectables.packageMan = packagemanager
	}
}

// WithInitSystem is a functional option that sets the init system to use for the connection.
func WithInitSystem(initsystem initsystem.ServiceManager) Option {
	return func(o *Options) {
		o.ConnectionInjectables.initSys = initsystem
	}
}

// WithInitSystemRepositeory is a functional option that sets the init system repository to use for the connection.
func WithInitSystemRepositeory(initsysRepo initsystemRepository) Option {
	return func(o *Options) {
		o.ConnectionInjectables.repositories.initsysRepo = initsysRepo
	}
}

// WithPackageManagerRepository is a functional option that sets the package manager repository to use for the connection.
func WithPackageManagerRepository(packagemanRepo packagemanagerRepository) Option {
	return func(o *Options) {
		o.ConnectionInjectables.repositories.packagemanRepo = packagemanRepo
	}
}

// WithSudoRepository is a functional option that sets the sudo repository to use for the connection.
func WithSudoRepository(sudoRepo sudoRepository) Option {
	return func(o *Options) {
		o.ConnectionInjectables.repositories.sudoRepo = sudoRepo
	}
}

// WithFsysRepository is a functional option that sets the filesystem repository to use for the connection.
func WithFsysRepository(fsysRepo rigfsRepository) Option {
	return func(o *Options) {
		o.ConnectionInjectables.repositories.fsysRepo = fsysRepo
	}
}

// WithLoggerFactory is a functional option that sets the logger factory to use for creating a logger for the connection.
func WithLoggerFactory(loggerFactory LoggerFactory) Option {
	return func(o *Options) {
		o.ConnectionInjectables.repositories.loggerFactory = loggerFactory
	}
}

// NewOptions creates a new Options struct with the supplied options applied over the defaults.
func NewOptions(opts ...Option) *Options {
	options := &Options{
		ConnectionInjectables: DefaultConnectionInjectables(),
	}

	options.Apply(opts...)

	return options
}
