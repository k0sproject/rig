package rig

import "github.com/k0sproject/rig/exec"

// ClientConfigurer is an interface that can be used to configure a client. The Connect() function calls the Client() function
// to get a client to use for connecting.
type ClientConfigurer interface {
	String() string
	Client() (Client, error)
}

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

// NewOptions creates a new Options struct with the supplied options applied over the defaults.
func NewOptions(opts ...Option) *Options {
	options := &Options{
		ConnectionInjectables: DefaultConnectionInjectables(),
	}

	options.Apply(opts...)

	return options
}
