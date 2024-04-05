package ssh

import (
	"time"

	"github.com/k0sproject/rig/v2/log"
)

// Options for the SSH client.
type Options struct {
	log.LoggerInjectable
	KeepAliveInterval *time.Duration
	ConfigParser      ConfigParser

	funcs []Option
}

func (o *Options) Funcs() []Option {
	return o.funcs
}

// Option is a function that sets some option on the Options struct.
type Option func(*Options)

// NewOptions creates a new Options struct with the given options applied.
func NewOptions(opts ...Option) *Options {
	o := &Options{}
	for _, opt := range opts {
		o.funcs = append(o.funcs, opt)
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

// WithConfigParser sets a custom ssh configuration parser.
func WithConfigParser(p ConfigParser) Option {
	return func(o *Options) {
		o.ConfigParser = p
	}
}
