//go:build !windows
// +build !windows

package ssh

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	ssh "golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

// captureSignals intercepts interrupt / resize signals and sends them over to the writer.
func captureSignals(stdin io.Writer, session *ssh.Session) func() {
	stopCh := make(chan struct{})
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTSTP, syscall.SIGWINCH)

	go func() {
		for sig := range sigCh {
			switch sig {
			case os.Interrupt:
				fmt.Fprintf(stdin, "\x03")
			case syscall.SIGTSTP:
				fmt.Fprintf(stdin, "\x1a")
			case syscall.SIGWINCH:
				_, err := session.SendRequest("window-change", false, termSizeWNCH())
				if err != nil {
					println("failed to relay window-change event: " + err.Error())
				}
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

func termSizeWNCH() []byte {
	size := make([]byte, 16)
	fd := int(os.Stdin.Fd())
	rows, cols, err := term.GetSize(fd)
	if err != nil {
		binary.BigEndian.PutUint32(size, 40)
		binary.BigEndian.PutUint32(size[4:], 80)
	} else {
		binary.BigEndian.PutUint32(size, uint32(cols))
		binary.BigEndian.PutUint32(size[4:], uint32(rows))
	}

	return size
}
