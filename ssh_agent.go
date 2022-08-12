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
		return nil, fmt.Errorf("- SSH_AUTH_SOCK is empty")
	}
	sshAgent, errConnect := net.Dial("unix", sshAgentSock)
	if errConnect != nil {
		return nil, fmt.Errorf("- can't connect to SSH agent: %s", errConnect)
	}
	signers, errGetSigners := agent.NewClient(sshAgent).Signers()
	if errGetSigners != nil {
		return nil, fmt.Errorf("- SSH agent: %s", errGetSigners)
	}
	if len(signers) > 0 {
		return signers, nil
	}
	return nil, fmt.Errorf("- SSH agent %s returned empty list", sshAgentSock)
}
