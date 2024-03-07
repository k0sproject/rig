package rig

import (
	"fmt"

	"github.com/k0sproject/rig/cmd"
	"github.com/k0sproject/rig/initsystem"
	"github.com/k0sproject/rig/log"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/packagemanager"
	"github.com/k0sproject/rig/protocol"
	"github.com/k0sproject/rig/remotefs"
	"github.com/k0sproject/rig/sudo"
)

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
	log.LoggerInjectable
	connection           protocol.Connection
	connectionConfigurer ConnectionConfigurer
	runner               cmd.Runner
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

func (p *packageManagerProvider) GetPackageManagerService(runner cmd.Runner) *packagemanager.Service {
	return packagemanager.NewPackageManagerService(p.provider, runner)
}

type initSystemProvider struct {
	provider initsystem.InitSystemProvider
}

func (p *initSystemProvider) GetInitSystemService(runner cmd.Runner) *initsystem.Service {
	return initsystem.NewInitSystemService(p.provider, runner)
}

type remoteFSProvider struct {
	provider remotefs.RemoteFSProvider
}

func (p *remoteFSProvider) GetRemoteFSService(runner cmd.Runner) *remotefs.Service {
	return remotefs.NewRemoteFSService(p.provider, runner)
}

type osReleaseProvider struct {
	provider os.OSReleaseProvider
}

func (p *osReleaseProvider) GetOSReleaseService(runner cmd.Runner) *os.Service {
	return os.NewOSReleaseService(p.provider, runner)
}

type sudoProvider struct {
	provider sudo.SudoProvider
}

func (p *sudoProvider) GetSudoService(runner cmd.Runner) *sudo.Service {
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
		runner:               o.runner,
		providersContainer:   o.providersContainer,
	}
}

// GetConnection returns the connection to use for the rig client. If no connection is set, it will use the ConnectionConfigurer to create one.
func (o *ClientOptions) GetConnection() (protocol.Connection, error) {
	var conn protocol.Connection
	if o.connection != nil {
		o.Log().Debug("using provided connection", log.HostAttr(o.connection), log.KeyComponent, "clientoptions")
		conn = o.connection
	} else {
		if o.connectionConfigurer == nil {
			return nil, fmt.Errorf("%w: no connection or connection configurer provided", protocol.ErrAbort)
		}
		o.Log().Debug("using client configurer to setup a connection", log.HostAttr(o.connectionConfigurer), log.KeyComponent, "clientoptions")
		c, err := o.connectionConfigurer.Connection()
		if err != nil {
			return nil, fmt.Errorf("create connection: %w", err)
		}
		o.Log().Debug("using connection received from client configurer", log.HostAttr(c), log.KeyComponent, "clientoptions")
		conn = c
	}

	log.InjectLogger(log.WithAttrs(o.Log(), log.HostAttr(conn), log.KeyProtocol, conn.Protocol()), conn)
	return conn, nil
}

// GetRunner returns the runner to use for the rig client.
func (o *ClientOptions) GetRunner(conn protocol.Connection) cmd.Runner {
	if o.runner != nil {
		return o.runner
	}
	runner := cmd.NewExecutor(conn)
	return runner
}

// ClientOption is a functional option type for the Options struct.
type ClientOption func(*ClientOptions)

// WithLogger is a functional option that sets the logger to use for the connection and its child components.
func WithLogger(logger log.Logger) ClientOption {
	return func(o *ClientOptions) {
		o.SetLogger(logger)
	}
}

// WithConnection is a functional option that sets the client to use for connecting instead of getting it from the ConnectionConfigurer.
func WithConnection(conn protocol.Connection) ClientOption {
	return func(o *ClientOptions) {
		o.connection = conn
	}
}

// WithRunner is a functional option that sets the runner to use for executing commands.
func WithRunner(runner cmd.Runner) ClientOption {
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

// DefaultClientOptions returns a new Options struct with the default options applied.
func DefaultClientOptions() *ClientOptions {
	return &ClientOptions{
		connectionConfigurer: defaultConnectionConfigurer(),
		providersContainer:   defaultProviders(),
	}
}

// NewClientOptions creates a new Options struct with the supplied options applied over the defaults.
func NewClientOptions(opts ...ClientOption) *ClientOptions {
	options := DefaultClientOptions()
	options.Apply(opts...)
	return options
}
