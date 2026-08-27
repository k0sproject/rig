package ssh

import (
	"time"

	"github.com/k0sproject/rig/v2/log"
)

// Options for the SSH client.
type Options struct {
	log.LoggerInjectable
	KeepAliveInterval *time.Duration
	IgnoreSSHConfig   bool
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

// WithKeepAlive sets the keep-alive interval option.
func WithKeepAlive(d time.Duration) Option {
	return func(o *Options) {
		o.KeepAliveInterval = &d
	}
}

// WithoutSSHConfig disables reading the OpenSSH client configuration files
// (~/.ssh/config and the system-wide ssh_config), mirroring "ssh -F none".
// OpenSSH's built-in defaults still apply. This is the functional-option
// equivalent of setting Config.IgnoreSSHConfig.
func WithoutSSHConfig() Option {
	return func(o *Options) {
		o.IgnoreSSHConfig = true
	}
}
