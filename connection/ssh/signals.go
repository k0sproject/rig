// +build !windows

package ssh

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	log "github.com/sirupsen/logrus"
	ssh "golang.org/x/crypto/ssh"
)

func (c *Connection) captureSignals(stdin io.WriteCloser, session *ssh.Session) {
	ch := make(chan os.Signal)
	signal.Notify(ch, os.Interrupt, syscall.SIGTSTP, syscall.SIGWINCH)

	go func() {
		for sig := range ch {
			switch sig {
			case os.Interrupt:
				log.Tracef("relaying ctrl-c to session")
				fmt.Fprintf(stdin, "\x03")
			case syscall.SIGTSTP:
				log.Tracef("relaying ctrl-z to session")
				fmt.Fprintf(stdin, "\x1a")
			case syscall.SIGWINCH:
				log.Tracef("relaying window size change")
				session.SendRequest("window-change", false, termSizeWNCH())
			}
		}
	}()
}
