package rig

import (
	"github.com/k0sproject/rig/client"
	"github.com/k0sproject/rig/exec"
)

type Client struct {
	*exec.Runner
	client.Connection
}

func NewClient(config clientConfigurer, opts ...Option) (*Client, error) {
	options := NewOptions(opts...)

	var conn client.Connection

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
