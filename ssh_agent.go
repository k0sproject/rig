//go:build !windows

package rig

import (
	"fmt"
	"net"
	"os"

	ssh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// getSshAgentSigners returns non empty list of signers from a SSH agent
func getSshAgentSigners() ([]ssh.Signer, error) {
	sshAgentSock := os.Getenv("SSH_AUTH_SOCK")
	if sshAgentSock == "" {
		return nil, fmt.Errorf("SSH_AUTH_SOCK is empty")
	}
	sshAgent, err := net.Dial("unix", sshAgentSock)
	if err != nil {
		return nil, fmt.Errorf("can't connect to SSH agent: %w", err)
	}
	signers, err := agent.NewClient(sshAgent).Signers()
	if err != nil {
		return nil, fmt.Errorf("SSH agent new client: %w", err)
	}
	return signers, nil
}
