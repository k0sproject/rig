package rig

import "github.com/k0sproject/rig/exec"

type ClientConfigurer interface {
	String() string
	Client() (Client, error)
}

type Options struct {
	*ConnectionInjectables
}

func (o *Options) Apply(opts ...Option) {
	for _, opt := range opts {
		opt(o)
	}
}

type Option func(*Options)

func WithClient(client Client) Option {
	return func(o *Options) {
		o.client = client
	}
}

func WithRunner(runner exec.Runner) Option {
	return func(o *Options) {
		o.ConnectionInjectables.Runner = runner
	}
}

func NewOptions(opts ...Option) *Options {
	options := &Options{
		ConnectionInjectables: DefaultConnectionInjectables(),
	}

	options.Apply(opts...)

	return options
}
