//go:build windows

package agent

import (
	"errors"
	"fmt"
	"github.com/Microsoft/go-winio"
	"github.com/davidmz/go-pageant"
	"golang.org/x/crypto/ssh/agent"
)

const (
	openSshAgentPipe = `\\.\pipe\openssh-ssh-agent`
)

// ErrSSHAgent is returned when connection to SSH agent fails
var ErrSSHAgent = errors.New("connect win ssh agent")

// NewClient on windows returns a pageant client or an open SSH agent client, whichever is available
func NewClient() (agent.Agent, error) {
	if pageant.Available() {
		return pageant.New(), nil
	}
	sock, err := winio.DialPipe(openSshAgentPipe, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: can't connect to ssh agent: %w", ErrSSHAgent, err)
	}
	return agent.NewClient(sock), nil
}
