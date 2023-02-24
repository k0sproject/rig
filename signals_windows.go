//go:build windows
// +build windows

package rig

import (
	"fmt"
	"io"
	"os"
	"os/signal"

	ssh "golang.org/x/crypto/ssh"
)

func captureSignals(stdin io.Writer, session *ssh.Session) func() {
	stopCh := make(chan struct{})
	sigCh := make(chan os.Signal)
	signal.Notify(ch, os.Interrupt)

	go func() {
		for sig := range sigCh {
			switch sig {
			case os.Interrupt:
				fmt.Fprintf(stdin, "\x03")
			}
		}
	}()

	go func() {
		<-stopCh
		signal.Stop(sigCh)
		close(sigCh)
	}()

	return func() { close(stopCh) }
}
