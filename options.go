package rig

import (
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/log"
)

type Option func(*Options)
type Options struct {
	Client   exec.Client
	logger   *log.Logger
	ExecOpts []exec.Option
}

func (o *Options) Logger() *log.Logger {
	if o.logger == nil {
		o.logger = log.NewLogger()
	}
	return o.logger
}

// WithLocalhost sets the localhost connection configuration
func WithClient(client exec.Client) Option {
	return func(o *Options) {
		o.Client = client
	}
}

func WithExecOptions(opts ...exec.Option) Option {
	return func(o *Options) {
		o.ExecOpts = opts
	}
}

func WithLogger(l *log.Logger) Option {
	return func(o *Options) {
		o.logger = l
	}
}

// NewOptions creates a new Options instance
func NewOptions(opts ...Option) *Options {
	o := &Options{}

	for _, opt := range opts {
		opt(o)
	}
	return o
}
