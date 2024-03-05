package ssh

import "github.com/k0sproject/rig/log"

type Options struct {
	log.LoggerInjectable
}

type Option func(*Options)

func NewOptions(opts ...Option) *Options {
	o := &Options{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

func WithLogger(l log.Logger) Option {
	return func(o *Options) {
		o.SetLogger(l)
	}
}
