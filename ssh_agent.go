//go:build !windows

package rig

import (
	"fmt"
	"net"
	"os"

	"github.com/k0sproject/rig/log"
	"golang.org/x/crypto/ssh/agent"
)

// ErrNoSSHAgent is returned when SSH_AUTH_SOCK is not set
var ErrNoSSHAgent = fmt.Errorf("no ssh agent found")

func agentClient() (agent.Agent, error) {
	sshAgentSock := os.Getenv("SSH_AUTH_SOCK")
	if sshAgentSock == "" {
		return nil, ErrNoSSHAgent
	}
	log.Debugf("using SSH_AUTH_SOCK=%s", sshAgentSock)
	sshAgent, err := net.Dial("unix", sshAgentSock)
	if err != nil {
		return nil, fmt.Errorf("can't connect to SSH agent: %w", err)
	}
	return agent.NewClient(sshAgent), nil
}
