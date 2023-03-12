package rig

import (
	"fmt"
	"net"

	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/log"
	"github.com/k0sproject/rig/protocol/localhost"
	"github.com/k0sproject/rig/protocol/ssh"
	"github.com/k0sproject/rig/protocol/winrm"
)

type connection interface {
	Protocol() string
	Address() net.Addr
	String() string
	Disconnect() error
}

type Client struct {
	*exec.Runner
	connection
}

func NewClient(config Config, opts ...Option) (*Client, error) {
	options := NewOptions(opts...)

	var conn exec.Client

	if options.Client != nil {
		conn = options.Client
	} else {
		var err error
		if config.SSH != nil {
			conn, err = ssh.NewClient(config.SSH)
		} else if config.WinRM != nil {
			conn, err = winrm.NewClient(config.WinRM)
		} else if config.Localhost != nil && config.Localhost.Enabled {
			conn, err = &localhost.Client{}, nil
		} else {
			return nil, fmt.Errorf("no suitable connection configuration provided")
		}

		if err != nil {
			return nil, err
		}
	}

	runner := exec.NewRunner(conn, options.ExecOpts...)
	runner.SetLogger(
		options.Logger().With(
			log.String("client", conn.String()),
		).WithGroup("runner"),
	)

	return &Client{runner, conn.(connection)}, nil
}
