// +build !windows

package ssh

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	ssh "golang.org/x/crypto/ssh"
)

func (c *Client) captureSignals(stdin io.WriteCloser, session *ssh.Session) {
	ch := make(chan os.Signal)
	signal.Notify(ch, os.Interrupt, syscall.SIGTSTP, syscall.SIGWINCH)

	go func() {
		for sig := range ch {
			switch sig {
			case os.Interrupt:
				fmt.Fprintf(stdin, "\x03")
			case syscall.SIGTSTP:
				fmt.Fprintf(stdin, "\x1a")
			case syscall.SIGWINCH:
				session.SendRequest("window-change", false, termSizeWNCH())
			}
		}
	}()
}
