//go:build !windows

package rig

import (
	"net"
	"os"

	"github.com/k0sproject/rig/errstring"
	"github.com/k0sproject/rig/log"
	"golang.org/x/crypto/ssh/agent"
)

// ErrSSHAgent is returned when connection to SSH agent fails
var ErrSSHAgent = errstring.New("connect ssh agent")

func agentClient() (agent.Agent, error) {
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
