package rig

import (
	"github.com/k0sproject/rig/client"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/log"
)

type Option func(*Options)
type Options struct {
	Client       exec.Client
	logger       *log.Logger
	PasswordFunc func() (string, error)
	execOpts     []exec.Option
	clientOpts   []client.Option
}

func (o *Options) Apply(opts ...Option) *Options {
	for _, opt := range opts {
		opt(o)
	}
	return o
}

func (o *Options) Logger() *log.Logger {
	if o.logger == nil {
		o.logger = log.Default()
	}
	return o.logger
}

func (o *Options) ClientOptions() []client.Option {
	var opts []client.Option
	if o.PasswordFunc != nil {
		opts = append(opts, client.PasswordCallback(o.PasswordFunc))
	}
	if o.Logger != nil {
		opts = append(opts, client.WithLogger(o.Logger()))
	}
	opts = append(opts, o.clientOpts...)
	return opts
}

func (o *Options) ExecOptions() []exec.Option {
	var opts []exec.Option
	if o.PasswordFunc != nil {
		opts = append(opts, exec.WithPasswordCallback(o.PasswordFunc))
	}
	if o.Logger != nil {
		opts = append(opts, exec.WithLogger(o.Logger()))
	}
	opts = append(opts, o.execOpts...)
	return opts
}

func DefaultOptions() *Options {
	return &Options{
		logger:     log.Default(),
		execOpts:   make([]exec.Option, 0),
		clientOpts: make([]client.Option, 0),
	}
}

// WithLocalhost sets the localhost connection configuration
func WithClient(client exec.Client) Option {
	return func(o *Options) {
		if len(o.clientOpts) > 0 {
			o.Logger().Warn("client options are ignored when client is set")
		}
		o.Client = client
	}
}

func WithClientOptions(opts ...client.Option) Option {
	return func(o *Options) {
		if o.Client != nil {
			o.Logger().Warn("client options are ignored when client is set")
		}
		o.clientOpts = append(o.clientOpts, opts...)
	}
}

func WithExecOptions(opts ...exec.Option) Option {
	return func(o *Options) {
		o.execOpts = append(o.execOpts, opts...)
	}
}

func WithLogger(l *log.Logger) Option {
	return func(o *Options) {
		o.logger = l
	}
}

func WithPasswordCallback(fn func() (string, error)) Option {
	return func(o *Options) {
		o.PasswordFunc = fn
	}
}

// NewOptions creates a new Options instance
func NewOptions(opts ...Option) *Options {
	return DefaultOptions().Apply(opts...)
}
