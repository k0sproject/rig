package ssh

import "github.com/k0sproject/rig/log"

// Options for the SSH client.
type Options struct {
	log.LoggerInjectable
}

// Option is a function that sets some option on the Options struct.
type Option func(*Options)

// NewOptions creates a new Options struct with the given options applied.
func NewOptions(opts ...Option) *Options {
	o := &Options{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// WithLogger sets the logger option.
func WithLogger(l log.Logger) Option {
	return func(o *Options) {
		o.SetLogger(l)
	}
}
