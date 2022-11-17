//go:build windows

package rig

import (
	"github.com/Microsoft/go-winio"
	"github.com/davidmz/go-pageant"
	"golang.org/x/crypto/ssh/agent"
)

const (
	openSshAgentPipe = `\\.\pipe\openssh-ssh-agent`
)

func agentClient() (agent.Agent, error) {
	if pageant.Available() {
		return pageant.New(), nil
	}
	sock, err := winio.DialPipe(openSshAgentPipe, nil)
	if err != nil {
		return nil, err
	}
	return agent.NewClient(sock), nil
}
