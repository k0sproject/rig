package rig

import (
	"fmt"

	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/initsystem"
	"github.com/k0sproject/rig/v2/log"
	"github.com/k0sproject/rig/v2/os"
	"github.com/k0sproject/rig/v2/packagemanager"
	"github.com/k0sproject/rig/v2/protocol"
	"github.com/k0sproject/rig/v2/remotefs"
	"github.com/k0sproject/rig/v2/sudo"
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
	retryConnection      bool
	providersContainer
}

type providersContainer struct {
	packageManagerProviderConfig
	initSystemProviderConfig
	remoteFSProviderConfig
	osReleaseProviderConfig
	sudoProviderConfig
}

type packageManagerProviderConfig struct {
	provider packagemanager.PackageManagerProvider
}

func (p *packageManagerProviderConfig) GetPackageManagerProvider(runner cmd.Runner) *packagemanager.Provider {
	return packagemanager.NewPackageManagerProvider(p.provider, runner)
}

type initSystemProviderConfig struct {
	provider initsystem.InitSystemProvider
}

func (p *initSystemProviderConfig) GetInitSystemProvider(runner cmd.Runner) *initsystem.Provider {
	return initsystem.NewInitSystemProvider(p.provider, runner)
}

type remoteFSProviderConfig struct {
	provider remotefs.RemoteFSProvider
}

func (p *remoteFSProviderConfig) GetRemoteFSProvider(runner cmd.Runner) *remotefs.Provider {
	return remotefs.NewRemoteFSProvider(p.provider, runner)
}

type osReleaseProviderConfig struct {
	provider os.OSReleaseProvider
}

func (p *osReleaseProviderConfig) GetOSReleaseProvider(runner cmd.Runner) *os.Provider {
	return os.NewOSReleaseProvider(p.provider, runner)
}

type sudoProviderConfig struct {
	provider sudo.SudoProvider
}

func (p *sudoProviderConfig) GetSudoProvider(runner cmd.Runner) *sudo.Provider {
	return sudo.NewSudoProvider(p.provider, runner)
}

func defaultProviders() providersContainer {
	return providersContainer{
		packageManagerProviderConfig: packageManagerProviderConfig{provider: packagemanager.DefaultRegistry()},
		initSystemProviderConfig:     initSystemProviderConfig{provider: initsystem.DefaultRegistry()},
		remoteFSProviderConfig:       remoteFSProviderConfig{provider: remotefs.DefaultRegistry()},
		osReleaseProviderConfig:      osReleaseProviderConfig{provider: os.DefaultRegistry()},
		sudoProviderConfig:           sudoProviderConfig{provider: sudo.DefaultRegistry()},
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

// ShouldRetry returns whether the connection should be retried.
func (o *ClientOptions) ShouldRetry() bool {
	return o.retryConnection
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

// WithRemoteFSProvider is a functional option that sets the filesystem provider to use for the connection's RemoteFSProvider.
func WithRemoteFSProvider(provider remotefs.RemoteFSProvider) ClientOption {
	return func(o *ClientOptions) {
		o.remoteFSProviderConfig = remoteFSProviderConfig{provider: provider}
	}
}

// WithInitSystemProvider is a functional option that sets the init system provider to use for the connection's InitSystemProvider.
func WithInitSystemProvider(provider initsystem.InitSystemProvider) ClientOption {
	return func(o *ClientOptions) {
		o.initSystemProviderConfig = initSystemProviderConfig{provider: provider}
	}
}

// WithOSReleaseProvider is a functional option that sets the os release provider to use for the connection's OSReleaseProvider.
func WithOSReleaseProvider(provider os.OSReleaseProvider) ClientOption {
	return func(o *ClientOptions) {
		o.osReleaseProviderConfig = osReleaseProviderConfig{provider: provider}
	}
}

// WithPackageManagerProvider is a functional option that sets the package manager provider to use for the connection's PackageManagerProvider.
func WithPackageManagerProvider(provider packagemanager.PackageManagerProvider) ClientOption {
	return func(o *ClientOptions) {
		o.packageManagerProviderConfig = packageManagerProviderConfig{provider: provider}
	}
}

// WithSudoProvider is a functional option that sets the sudo provider to use for the connection's SudoProvider.
func WithSudoProvider(provider sudo.SudoProvider) ClientOption {
	return func(o *ClientOptions) {
		o.sudoProviderConfig = sudoProviderConfig{provider: provider}
	}
}

// WithRetry is a functional option that toggles the connection retry feature. Default is true.
func WithRetry(retry bool) ClientOption {
	return func(o *ClientOptions) {
		o.retryConnection = retry
	}
}

// DefaultClientOptions returns a new Options struct with the default options applied.
func DefaultClientOptions() *ClientOptions {
	return &ClientOptions{
		connectionConfigurer: defaultConnectionConfigurer(),
		providersContainer:   defaultProviders(),
		retryConnection:      true,
	}
}

// NewClientOptions creates a new Options struct with the supplied options applied over the defaults.
func NewClientOptions(opts ...ClientOption) *ClientOptions {
	options := DefaultClientOptions()
	options.Apply(opts...)
	return options
}
