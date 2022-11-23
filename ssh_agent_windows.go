//go:build windows

package rig

import (
	"github.com/Microsoft/go-winio"
	"github.com/davidmz/go-pageant"
	"github.com/k0sproject/rig/errstring"
	"golang.org/x/crypto/ssh/agent"
)

const (
	openSshAgentPipe = `\\.\pipe\openssh-ssh-agent`
)

// ErrSSHAgent is returned when connection to SSH agent fails
var ErrSSHAgent = errstring.New("connect win ssh agent")

func agentClient() (agent.Agent, error) {
	if pageant.Available() {
		return pageant.New(), nil
	}
	sock, err := winio.DialPipe(openSshAgentPipe, nil)
	if err != nil {
		return nil, ErrSSHAgent.Wrapf("can't connect to ssh agent: %w", err)
	}
	return agent.NewClient(sock), nil
}
