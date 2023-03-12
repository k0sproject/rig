package rigtest

import (
	"bytes"
	"errors"
	"io"
	"net"

	"github.com/k0sproject/rig/exec"
)

var address = &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}

type MockResponse struct {
	Stdout string
	Stderr string
	Fail   bool
}

type MockClient struct {
	Windows   bool
	Responses map[string]MockResponse
}

type Waiter struct {
	fail  bool
	stdin *bytes.Buffer
}

func (w *Waiter) Wait() error {
	if w.fail {
		return errors.New("failed by request")
	}
	return nil
}

func (w *Waiter) Stdin() string {
	return w.stdin.String()
}

func (m MockClient) IsWindows() bool {
	return m.Windows
}

func (m MockClient) Address() net.Addr {
	return address
}

func (m MockClient) String() string {
	return "rigtest.MockClient"
}

func (m MockClient) Disconnect() error {
	return nil
}

func (m MockClient) AddResponse(cmd string, resp MockResponse) {
	if m.Responses == nil {
		m.Responses = make(map[string]MockResponse)
	}
	m.Responses[cmd] = resp
}

func (m MockClient) Exec(cmd string, stdin io.Reader, stdout, stderr io.Writer) (exec.Process, error) {
	proc := &Waiter{stdin: &bytes.Buffer{}}
	if m.Responses != nil {
		if resp, ok := m.Responses[cmd]; ok {
			if resp.Fail {
				proc.fail = true
			}
			stdout.Write([]byte(resp.Stdout))
			stderr.Write([]byte(resp.Stderr))
			_, _ = io.Copy(proc.stdin, stdin)
		}
	}

	return proc, nil
}
