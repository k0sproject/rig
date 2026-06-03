//go:build !windows

// Package agent provides a client implementation for the SSH agent.
package agent

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"

	"golang.org/x/crypto/ssh/agent"
)

// ErrSSHAgent is returned when connection to SSH agent fails.
var ErrSSHAgent = errors.New("connect ssh agent")

// NewClient returns an SSH agent and its underlying closer if a socket address is defined in SSH_AUTH_SOCK.
// The caller must close the returned io.Closer when done.
func NewClient() (agent.Agent, io.Closer, error) {
	sshAgentSock := os.Getenv("SSH_AUTH_SOCK")
	if sshAgentSock == "" {
		return nil, nil, fmt.Errorf("%w: SSH_AUTH_SOCK is not set", ErrSSHAgent)
	}
	sshAgent, err := net.Dial("unix", sshAgentSock) //nolint:gosec,noctx // G704: SSH_AUTH_SOCK is safe; no context available in this function
	if err != nil {
		return nil, nil, fmt.Errorf("%w: can't connect to ssh agent: %w", ErrSSHAgent, err)
	}
	return agent.NewClient(sshAgent), sshAgent, nil
}

// NewClientFromSocket returns an SSH agent and its underlying closer using the
// provided socket path. The caller must close the returned io.Closer when done.
func NewClientFromSocket(socketPath string) (agent.Agent, io.Closer, error) {
	conn, err := net.Dial("unix", socketPath) //nolint:noctx // no context available in this function
	if err != nil {
		return nil, nil, fmt.Errorf("%w: can't connect to ssh agent at %q: %w", ErrSSHAgent, socketPath, err)
	}
	return agent.NewClient(conn), conn, nil
}
