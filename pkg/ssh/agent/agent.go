//go:build !windows

// Package agent provides an implementation of the SSH agent protocol.
package agent

import (
	"net"
	"os"

	"github.com/k0sproject/rig/errstring"
	"github.com/k0sproject/rig/log"
	"golang.org/x/crypto/ssh/agent"
)

// ErrSSHAgent is returned when connection to SSH agent fails
var ErrSSHAgent = errstring.New("connect ssh agent")

// NewClient returns an SSH agent if a socket address is defined in SSH_AUTH_SOCK environment variable
func NewClient() (agent.Agent, error) {
	sshAgentSock := os.Getenv("SSH_AUTH_SOCK")
	if sshAgentSock == "" {
		return nil, ErrSSHAgent.Wrapf("SSH_AUTH_SOCK is not set")
	}
	log.Debugf("using SSH_AUTH_SOCK=%s", sshAgentSock)
	sshAgent, err := net.Dial("unix", sshAgentSock)
	if err != nil {
		return nil, ErrSSHAgent.Wrapf("can't connect to ssh agent: %w", err)
	}
	return agent.NewClient(sshAgent), nil
}
