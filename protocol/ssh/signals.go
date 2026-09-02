//go:build !windows

package ssh

import (
	"context"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/k0sproject/rig/v2/log"
	ssh "golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

// captureSignals relays local interrupt, suspend and window-resize signals to a
// remote session and returns a function that stops relaying.
//
// tty is the local terminal the session's pty was created from, or nil when the
// session has no pty, in which case nothing is relayed and the returned stop
// function has nothing to undo.
//
// Interrupt and suspend are sent as the control characters a terminal would
// send rather than through [ssh.Session.Signal]. The "signal" channel request
// that Signal makes (RFC 4254 section 6.9) is optional for a server to act on,
// and OpenSSH's sshd does not deliver it to the session's process, so relaying
// through it would quietly do nothing. A \x03 or \x1a written to the session
// instead reaches the remote pty's line discipline, which raises SIGINT or
// SIGTSTP in the foreground process group exactly as a local terminal does.
//
// That mechanism is also why the relay needs a pty: without one there is no
// line discipline to read the bytes as anything but data, so injecting them
// would corrupt the stdin the caller is piping, and a resize would describe a
// terminal the remote end does not have.
func captureSignals(ctx context.Context, stdin io.Writer, session *ssh.Session, tty *os.File) func() {
	if tty == nil {
		return func() {}
	}

	stopCh := make(chan struct{})
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTSTP, syscall.SIGWINCH)

	go func() {
		for sig := range sigCh {
			switch sig {
			case os.Interrupt:
				writeControlChar(ctx, stdin, "\x03", "interrupt")
			case syscall.SIGTSTP:
				writeControlChar(ctx, stdin, "\x1a", "suspend")
			case syscall.SIGWINCH:
				relayWindowChange(ctx, session, tty)
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

// writeControlChar sends one control character to the remote pty, tracing a
// failure rather than failing the session: the command is still running, and
// there is nobody to return an error to from a signal handler.
func writeControlChar(ctx context.Context, stdin io.Writer, char, name string) {
	if _, err := io.WriteString(stdin, char); err != nil {
		log.Trace(ctx, "failed to relay "+name+" to the remote session", log.KeyError, err)
	}
}

// relayWindowChange tells the remote pty the terminal's new size.
//
// The size is read from the same terminal the pty was created from, which is
// not necessarily os.Stdin. A size that cannot be read is not reported at all,
// since there is nothing true to send: any made-up geometry would be worse than
// leaving the remote end with the last one it was told.
func relayWindowChange(ctx context.Context, session *ssh.Session, tty *os.File) {
	cols, rows, err := term.GetSize(int(tty.Fd()))
	if err != nil {
		log.Trace(ctx, "not relaying window-change, cannot read the local terminal size", log.KeyError, err)
		return
	}
	if err := session.WindowChange(rows, cols); err != nil {
		log.Trace(ctx, "failed to relay window-change event", log.KeyError, err)
	}
}
