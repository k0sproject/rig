package rig

import (
	"fmt"

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

// ClientOptions is a struct that holds the variadic options for the rig package.
type ClientOptions struct {
	connection           protocol.Connection
	connectionConfigurer ConnectionConfigurer
	loggerFactory        LoggerFactory
	runner               exec.Runner
	providersContainer
}

type providersContainer struct {
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

func defaultProviders() providersContainer {
	return providersContainer{
		packageManagerProvider: packageManagerProvider{provider: packagemanager.DefaultProvider()},
		initSystemProvider:     initSystemProvider{provider: initsystem.DefaultProvider()},
		remoteFSProvider:       remoteFSProvider{provider: remotefs.DefaultProvider()},
		osReleaseProvider:      osReleaseProvider{provider: os.DefaultProvider()},
		sudoProvider:           sudoProvider{provider: sudo.DefaultProvider()},
	}
}

// Apply applies the supplied options to the Options struct.
func (o *ClientOptions) Apply(opts ...ClientOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// Validate the options.
func (o *ClientOptions) Validate() error {
	if o.connection == nil && o.connectionConfigurer == nil {
		return fmt.Errorf("%w: no connection or connection configurer provided", protocol.ErrValidationFailed)
	}
	return nil
}

// Clone returns a copy of the Options struct.
func (o *ClientOptions) Clone() *ClientOptions {
	return &ClientOptions{
		connection:           o.connection,
		connectionConfigurer: o.connectionConfigurer,
		loggerFactory:        o.loggerFactory,
		runner:               o.runner,
		providersContainer:   o.providersContainer,
	}
}

// GetConnection returns the connection to use for the rig client. If no connection is set, it will use the ConnectionConfigurer to create one.
func (o *ClientOptions) GetConnection() (protocol.Connection, error) {
	if o.connection != nil {
		return o.connection, nil
	}
	if o.connectionConfigurer == nil {
		return nil, fmt.Errorf("%w: no connection or connection configurer provided", protocol.ErrAbort)
	}
	conn, err := o.connectionConfigurer.Connection()
	if err != nil {
		return nil, fmt.Errorf("create connection: %w", err)
	}
	return conn, nil
}

// GetRunner returns the runner to use for the rig client.
func (o *ClientOptions) GetRunner(conn protocol.Connection) exec.Runner {
	if o.runner != nil {
		return o.runner
	}
	return exec.NewHostRunner(conn)
}

// ClientOption is a functional option type for the Options struct.
type ClientOption func(*ClientOptions)

// WithConnection is a functional option that sets the client to use for connecting instead of getting it from the ConnectionConfigurer.
func WithConnection(conn protocol.Connection) ClientOption {
	return func(o *ClientOptions) {
		o.connection = conn
	}
}

// WithRunner is a functional option that sets the runner to use for executing commands.
func WithRunner(runner exec.Runner) ClientOption {
	return func(o *ClientOptions) {
		o.runner = runner
	}
}

// WithConnectionConfigurer is a functional option that sets the client configurer to use for connecting.
func WithConnectionConfigurer(configurer ConnectionConfigurer) ClientOption {
	return func(o *ClientOptions) {
		o.connectionConfigurer = configurer
	}
}

// WithRemoteFSProvider is a functional option that sets the filesystem provider to use for the connection's RemoteFSService.
func WithRemoteFSProvider(provider remotefs.RemoteFSProvider) ClientOption {
	return func(o *ClientOptions) {
		o.providersContainer.remoteFSProvider = remoteFSProvider{provider: provider}
	}
}

// WithInitSystemProvider is a functional option that sets the init system provider to use for the connection's InitSystemService.
func WithInitSystemProvider(provider initsystem.InitSystemProvider) ClientOption {
	return func(o *ClientOptions) {
		o.providersContainer.initSystemProvider = initSystemProvider{provider: provider}
	}
}

// WithOSReleaseProvider is a functional option that sets the os release provider to use for the connection's OSReleaseService.
func WithOSReleaseProvider(provider os.OSReleaseProvider) ClientOption {
	return func(o *ClientOptions) {
		o.providersContainer.osReleaseProvider = osReleaseProvider{provider: provider}
	}
}

// WithPackageManagerProvider is a functional option that sets the package manager provider to use for the connection's PackageManagerService.
func WithPackageManagerProvider(provider packagemanager.PackageManagerProvider) ClientOption {
	return func(o *ClientOptions) {
		o.providersContainer.packageManagerProvider = packageManagerProvider{provider: provider}
	}
}

// WithSudoProvider is a functional option that sets the sudo provider to use for the connection's SudoService.
func WithSudoProvider(provider sudo.SudoProvider) ClientOption {
	return func(o *ClientOptions) {
		o.providersContainer.sudoProvider = sudoProvider{provider: provider}
	}
}

// WithLoggerFactory is a functional option that sets the logger factory to use for creating a logger for the connection.
func WithLoggerFactory(loggerFactory LoggerFactory) ClientOption {
	return func(o *ClientOptions) {
		o.loggerFactory = loggerFactory
	}
}

// DefaultClientOptions returns a new Options struct with the default options applied.
func DefaultClientOptions() *ClientOptions {
	return &ClientOptions{
		connectionConfigurer: defaultConnectionConfigurer(),
		loggerFactory:        defaultLoggerFactory,
		providersContainer:   defaultProviders(),
	}
}

// NewClientOptions creates a new Options struct with the supplied options applied over the defaults.
func NewClientOptions(opts ...ClientOption) *ClientOptions {
	options := DefaultClientOptions()
	options.Apply(opts...)
	return options
}