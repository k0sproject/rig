//go:build !windows

// Package agent provides an implementation of the SSH agent protocol.
package agent

import (
	"errors"
	"fmt"
	"net"
	"os"

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
	sshAgent, err := net.Dial("unix", sshAgentSock)
	if err != nil {
		return nil, fmt.Errorf("%w: can't connect to ssh agent: %w", ErrSSHAgent, err)
	}
	return agent.NewClient(sshAgent), nil
}
