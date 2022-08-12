//go:build windows

package rig

import (
	"fmt"
	"strings"

	"github.com/Microsoft/go-winio"
	"github.com/davidmz/go-pageant"
	ssh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

const (
	openSshAgentPipe = `\\.\pipe\openssh-ssh-agent`
)

// getSshAgentSigners returns non empty list of signers from a SSH agent
func getSshAgentSigners() ([]ssh.Signer, error) {
	var (
		errors  []string
		signers []ssh.Signer
	)

	if pageant.Available() {
		signersPageant, errGetSigners := pageant.New().Signers()
		if errGetSigners != nil {
			errors = append(errors, fmt.Sprintf("- Failed to get signers from Pageant: %s", errGetSigners))
		} else {
			if len(signersPageant) > 0 {
				signers = append(signers, signersPageant...)
			} else {
				errors = append(errors, "- No keys loaded in Pageant")
			}
		}
	} else {
		errors = append(errors, "- Pageant is unavailable")
	}

	sock, err := winio.DialPipe(openSshAgentPipe, nil)
	if err != nil {
		errors = append(errors, fmt.Sprintf("- Can't connect to openssh-agent: %s", err))
	} else {
		signersOpenSSHAgent, errGetSigners := agent.NewClient(sock).Signers()
		if errGetSigners != nil {
			errors = append(errors, fmt.Sprintf("- Failed to get signers from openssh-agent: %s", errGetSigners))
		} else {
			if len(signersOpenSSHAgent) > 0 {
				signers = append(signers, signersOpenSSHAgent...)
			} else {
				errors = append(errors, "- No keys loaded in openssh-agent")
			}
		}
	}
	if len(signers) > 0 {
		return signers, nil
	}
	return nil, fmt.Errorf("%s", strings.Join(errors, "\n"))
}
