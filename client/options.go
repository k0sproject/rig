package client

import (
	"time"

	"github.com/k0sproject/rig/log"
)

// Option is a functional option type and it appears in function signatures mostly
// in the form of "...client.Option".
type Option func(*Options)

// Options describes client options
type Options struct {
	Logger           *log.Logger
	ConnectTimeout   time.Duration
	PasswordCallback func() (string, error)
}

// DefaultOptions returns default client options
func DefaultOptions() *Options {
	return &Options{
		Logger:         log.Default(),
		ConnectTimeout: 30 * time.Second,
	}
}

func (o *Options) clone() *Options {
	return &Options{
		Logger:         o.Logger,
		ConnectTimeout: o.ConnectTimeout,
	}
}

func (o *Options) With(opts ...Option) *Options {
	options := o.clone()
	for _, opt := range opts {
		opt(options)
	}
	return options
}

// WithLogger is an option that can be used to set the logger for the client.
func WithLogger(logger *log.Logger) Option {
	return func(opts *Options) {
		opts.Logger = logger
	}
}

// ConnectTimeout is an option that can be used to set the timeout for connecting to the remote host.
// todo: consider replacing this with a context
func ConnectTimeout(timeout time.Duration) Option {
	return func(opts *Options) {
		opts.ConnectTimeout = timeout
	}
}

// PasswordCallback is a function that can be used to ask for a password for a password prompt.
func PasswordCallback(callback func() (string, error)) Option {
	return func(opts *Options) {
		opts.PasswordCallback = callback
	}
}

// NewOptions creates a new Options struct with default values and applies the given extra options.
func NewOptions(opts ...Option) *Options {
	return DefaultOptions().With(opts...)
}
