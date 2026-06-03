//go:build windows

// Package agent provides a client implementation for the SSH agent.
package agent

import (
	"errors"
	"fmt"
	"io"

	"github.com/Microsoft/go-winio"
	"github.com/davidmz/go-pageant"
	"golang.org/x/crypto/ssh/agent"
)

const (
	openSshAgentPipe = `\\.\pipe\openssh-ssh-agent`
)

// ErrSSHAgent is returned when connection to SSH agent fails
var ErrSSHAgent = errors.New("connect win ssh agent")

// NewClient on windows returns a pageant client or an open SSH agent client, whichever is available.
// The caller must close the returned io.Closer when done (may be nil for pageant).
func NewClient() (agent.Agent, io.Closer, error) {
	if pageant.Available() {
		return pageant.New(), nil, nil
	}
	sock, err := winio.DialPipe(openSshAgentPipe, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: can't connect to ssh agent: %w", ErrSSHAgent, err)
	}
	return agent.NewClient(sock), sock, nil
}
