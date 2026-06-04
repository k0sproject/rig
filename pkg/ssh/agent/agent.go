//go:build !windows

// Package agent provides an implementation of the SSH agent protocol.
package agent

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"

	"github.com/k0sproject/rig/log"
	"golang.org/x/crypto/ssh/agent"
)

// ErrSSHAgent is returned when connection to SSH agent fails
var ErrSSHAgent = errors.New("connect ssh agent")

// NewClient returns an SSH agent if a socket address is defined in SSH_AUTH_SOCK environment variable
func NewClient() (agent.Agent, error) {
	sshAgentSock := os.Getenv("SSH_AUTH_SOCK")
	if sshAgentSock == "" {
		return nil, fmt.Errorf("%w: SSH_AUTH_SOCK is not set", ErrSSHAgent)
	}
	log.Debugf("using SSH_AUTH_SOCK=%s", sshAgentSock)
	sshAgent, err := (&net.Dialer{}).DialContext(context.Background(), "unix", sshAgentSock)
	if err != nil {
		return nil, fmt.Errorf("%w: can't connect to ssh agent: %w", ErrSSHAgent, err)
	}
	return agent.NewClient(sshAgent), nil
}
