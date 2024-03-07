package winrm

import "github.com/k0sproject/rig/log"

// Options for WinRM connetion.
type Options struct {
	log.LoggerInjectable
}

// Option is a functional option for WinRM [Options].
type Option func(*Options)

// NewOptions creates a new [Options] struct with the provided options.
func NewOptions(opts ...Option) *Options {
	o := &Options{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// WithLogger sets the logger for the WinRM client.
func WithLogger(l log.Logger) Option {
	return func(o *Options) {
		o.SetLogger(l)
	}
}
