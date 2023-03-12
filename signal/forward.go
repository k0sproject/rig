// Package signal provides a simple way to forward signals to a remote process
package signal

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/term"
)

type cancelFunc func()

type sshSession interface {
	SendRequest(string, bool, []byte) (bool, error)
}

// Forward intercepts interrupt / resize signals and sends them over to the writer
func Forward(out io.Writer, session sshSession) cancelFunc {
	stopCh := make(chan struct{})
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, TerminalSignals...)

	go func() {
		for sig := range sigCh {
			switch sig {
			case os.Interrupt:
				fmt.Fprintf(out, "\x03")
			case syscall.SIGTSTP:
				fmt.Fprintf(out, "\x1a")
			case syscall.SIGWINCH:
				if s, ok := session.(sshSession); ok {
					_, _ = s.SendRequest("window-change", false, termSizeWNCH())
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
