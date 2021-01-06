// +build windows

package ssh

import (
	"fmt"
	"io"
	"os"
	"os/signal"

	log "github.com/sirupsen/logrus"
	ssh "golang.org/x/crypto/ssh"
)

func (c *Connection) captureSignals(stdin io.WriteCloser, session *ssh.Session) {
	ch := make(chan os.Signal)
	signal.Notify(ch, os.Interrupt)

	go func() {
		for sig := range ch {
			switch sig {
			case os.Interrupt:
				log.Tracef("relaying ctrl-c to session")
				fmt.Fprintf(stdin, "\x03")
			}
		}
	}()
}
