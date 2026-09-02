//go:build windows

package ssh

import (
	"context"
	"io"
	"os"
	"os/signal"

	"github.com/k0sproject/rig/v2/log"
	ssh "golang.org/x/crypto/ssh"
)

// captureSignals relays local interrupts to a remote session and returns a
// function that stops relaying. See the unix build of this file for why the
// interrupt travels as a control character rather than through
// [ssh.Session.Signal], and why a pty is required for it to mean anything.
//
// Windows has no SIGTSTP or SIGWINCH, so an interrupt is all there is to relay.
func captureSignals(ctx context.Context, stdin io.Writer, _ *ssh.Session, tty *os.File) func() {
	if tty == nil {
		return func() {}
	}

	stopCh := make(chan struct{})
	// Buffered, because signal.Notify never blocks on a send: an unbuffered
	// channel drops any signal arriving while the relay is busy.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)

	go func() {
		for sig := range sigCh {
			if sig == os.Interrupt {
				if _, err := io.WriteString(stdin, "\x03"); err != nil {
					log.Trace(ctx, "failed to relay interrupt to the remote session", log.KeyError, err)
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
