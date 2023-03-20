package rig

import (
	"net"

	"github.com/k0sproject/rig/exec"
)

type connection interface {
	Protocol() string
	Address() net.Addr
	String() string
	Disconnect() error
}

type Client struct {
	*exec.Runner
	exec.Client
}

func NewClient(config Config, opts ...Option) (*Client, error) {
	options := NewOptions(opts...)

	var conn exec.Client

	if options.Client != nil {
		conn = options.Client
	} else {
		client, err := config.NewClient(options.ClientOptions()...)

		if err != nil {
			return nil, err
		}
		conn = client
	}

	runner := exec.NewRunner(conn, options.ExecOptions()...)
	return &Client{runner, conn}, nil
}
