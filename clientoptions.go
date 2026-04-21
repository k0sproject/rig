package rig

import (
	"errors"
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

var errNilOSRelease = errors.New("os release provider returned nil release without error")

// ConnectionFactory can create connections. When a connection is not given, the factory is used
// to build a connection.
type ConnectionFactory interface {
	fmt.Stringer
	Connection() (protocol.Connection, error)
}

func defaultConnectionFactory() ConnectionFactory {
	return &CompositeConfig{}
}

// ClientOptions is a struct that holds the variadic options for the rig package.
type ClientOptions struct {
	log.LoggerInjectable
	connection        protocol.Connection
	connectionFactory ConnectionFactory
	runner            cmd.Runner
	retryConnection   bool
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
	provider packagemanager.ManagerProvider
}

func (p *packageManagerProviderConfig) GetPackageManagerProvider(runner cmd.Runner) *packagemanager.Provider {
	return packagemanager.NewPackageManagerProvider(p.provider, runner)
}

type initSystemProviderConfig struct {
	provider initsystem.ServiceManagerProvider
}

func (p *initSystemProviderConfig) GetInitSystemProvider(runner cmd.Runner) *initsystem.Provider {
	return initsystem.NewInitSystemProvider(p.provider, runner)
}

type remoteFSProviderConfig struct {
	provider remotefs.FSProvider
}

func (p *remoteFSProviderConfig) GetRemoteFSProvider(runner cmd.Runner) *remotefs.Provider {
	return remotefs.NewRemoteFSProvider(p.provider, runner)
}

type osReleaseProviderConfig struct {
	provider   os.ReleaseProvider
	idOverride string
}

func (p *osReleaseProviderConfig) GetOSReleaseProvider(runner cmd.Runner) *os.Provider {
	provider := p.provider
	if p.idOverride != "" {
		idOverride := p.idOverride
		original := p.provider
		provider = func(r cmd.SimpleRunner) (*os.Release, error) {
			release, err := original(r)
			if err != nil {
				return nil, err
			}
			if release == nil {
				return nil, errNilOSRelease
			}
			result := *release
			result.ID = idOverride
			return &result, nil
		}
	}
	return os.NewOSReleaseProvider(provider, runner)
}

type sudoProviderConfig struct {
	provider sudo.RunnerProvider
}

func (p *sudoProviderConfig) GetSudoProvider(runner cmd.Runner) *sudo.Provider {
	return sudo.NewSudoProvider(p.provider, runner)
}

func defaultProviders() providersContainer {
	return providersContainer{
		packageManagerProviderConfig: packageManagerProviderConfig{provider: packagemanager.DefaultRegistry().Get},
		initSystemProviderConfig:     initSystemProviderConfig{provider: initsystem.DefaultRegistry().Get},
		remoteFSProviderConfig:       remoteFSProviderConfig{provider: remotefs.DefaultRegistry().Get},
		osReleaseProviderConfig:      osReleaseProviderConfig{provider: os.DefaultRegistry().Get},
		sudoProviderConfig:           sudoProviderConfig{provider: sudo.DefaultRegistry().Get},
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
	if o.connection == nil && o.connectionFactory == nil {
		return fmt.Errorf("%w: no connection or connection factory provided", protocol.ErrValidationFailed)
	}
	return nil
}

// Clone returns a copy of the Options struct.
func (o *ClientOptions) Clone() *ClientOptions {
	return &ClientOptions{
		LoggerInjectable:   o.LoggerInjectable,
		connection:         o.connection,
		connectionFactory:  o.connectionFactory,
		runner:             o.runner,
		retryConnection:    o.retryConnection,
		providersContainer: o.providersContainer,
	}
}

// ShouldRetry returns whether the connection should be retried.
func (o *ClientOptions) ShouldRetry() bool {
	return o.retryConnection
}

// GetConnection returns the connection to use for the rig client. If no connection is set, it will use the ConnectionFactory to create one.
func (o *ClientOptions) GetConnection() (protocol.Connection, error) {
	var conn protocol.Connection
	if o.connection != nil {
		o.Log().Debug("using provided connection", log.HostAttr(o.connection), log.KeyComponent, "clientoptions")
		conn = o.connection
	} else {
		if o.connectionFactory == nil {
			return nil, fmt.Errorf("%w: no connection or connection factory provided", protocol.ErrNonRetryable)
		}
		o.Log().Debug("using connection factory to setup a connection", log.HostAttr(o.connectionFactory), log.KeyComponent, "clientoptions")
		c, err := o.connectionFactory.Connection()
		if err != nil {
			return nil, fmt.Errorf("create connection: %w", err)
		}
		o.Log().Debug("using connection received from connection factory", log.HostAttr(c), log.KeyComponent, "clientoptions")
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

// WithConnection is a functional option that sets the client to use for connecting instead of getting it from the ConnectionFactory.
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

// WithConnectionFactory is a functional option that sets the connection factory to use for connecting.
func WithConnectionFactory(factory ConnectionFactory) ClientOption {
	return func(o *ClientOptions) {
		o.connectionFactory = factory
	}
}

// WithRemoteFSProvider is a functional option that sets the filesystem provider to use for the connection's RemoteFSProvider.
func WithRemoteFSProvider(provider remotefs.FSProvider) ClientOption {
	return func(o *ClientOptions) {
		o.remoteFSProviderConfig = remoteFSProviderConfig{provider: provider}
	}
}

// WithInitSystemProvider is a functional option that sets the init system provider to use for the connection's InitSystemProvider.
func WithInitSystemProvider(provider initsystem.ServiceManagerProvider) ClientOption {
	return func(o *ClientOptions) {
		o.initSystemProviderConfig = initSystemProviderConfig{provider: provider}
	}
}

// WithOSReleaseProvider is a functional option that sets the os release provider to use for the connection's OSReleaseProvider.
func WithOSReleaseProvider(provider os.ReleaseProvider) ClientOption {
	return func(o *ClientOptions) {
		o.osReleaseProviderConfig.provider = provider
	}
}

// WithOSIDOverride is a functional option that overrides the OS ID reported by the
// host's detected [os.Release]. Detection still runs normally; only the ID field of
// the result is replaced. IDLike from actual detection is preserved so callers can
// still inspect the distro family. This is useful when the detected ID does not match
// any known configurer but a compatible one is known (e.g. an unsupported derivative
// of a supported distro).
func WithOSIDOverride(id string) ClientOption {
	return func(o *ClientOptions) {
		o.idOverride = id
	}
}

// WithPackageManagerProvider is a functional option that sets the package manager provider to use for the connection's PackageManagerProvider.
func WithPackageManagerProvider(provider packagemanager.ManagerProvider) ClientOption {
	return func(o *ClientOptions) {
		o.packageManagerProviderConfig = packageManagerProviderConfig{provider: provider}
	}
}

// WithSudoProvider is a functional option that sets the sudo provider to use for the connection's SudoProvider.
func WithSudoProvider(provider sudo.RunnerProvider) ClientOption {
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
		connectionFactory:  defaultConnectionFactory(),
		providersContainer: defaultProviders(),
		retryConnection:    true,
	}
}

// NewClientOptions creates a new Options struct with the supplied options applied over the defaults.
func NewClientOptions(opts ...ClientOption) *ClientOptions {
	options := DefaultClientOptions()
	options.Apply(opts...)
	return options
}
