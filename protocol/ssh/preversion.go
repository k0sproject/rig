package ssh

import (
	"bytes"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
)

// maxPreVersionText bounds how much pre-version text an error message keeps.
// Across lines the end is kept rather than the start, because the line that
// explains a refusal is the last one a server sends before hanging up. Within a
// line the start is kept: x/crypto gives up on a line once it reaches this same
// 255 bytes, so it never reads further into one.
const maxPreVersionText = 255

var sshVersionPrefix = []byte("SSH-")

// preVersionRecorder wraps the connection an SSH handshake reads from and keeps
// the lines the server sends ahead of its version string.
//
// RFC 4253 section 4.2 lets a server send such lines and x/crypto discards them,
// but sshd uses that slot to explain a refusal: OpenSSH 9.8 and later write
// "Not allowed at this time" when PerSourcePenalties or MaxStartups drops a
// connection, and the handshake error that results says only EOF or
// "connection reset by peer".
type preVersionRecorder struct {
	net.Conn

	// versionSeen is read without mu so that, once the handshake is past the
	// version exchange, Read costs nothing for the rest of the session.
	versionSeen atomic.Bool

	mu    sync.Mutex
	lines []byte // the most recent completed lines, separated by '\n'
	line  []byte // the start of the line being read
}

func newPreVersionRecorder(conn net.Conn) *preVersionRecorder {
	return &preVersionRecorder{Conn: conn}
}

// Read reads from the wrapped connection and records what arrives before the
// version line.
func (r *preVersionRecorder) Read(p []byte) (int, error) {
	n, err := r.Conn.Read(p)
	if n > 0 && !r.versionSeen.Load() {
		r.record(p[:n])
	}
	return n, err //nolint:wrapcheck // io.Reader contract: callers compare against io.EOF, wrapping breaks it
}

func (r *preVersionRecorder) record(data []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.versionSeen.Load() {
		return
	}
	for _, char := range data {
		switch char {
		case '\n':
			if bytes.HasPrefix(r.line, sshVersionPrefix) {
				r.versionSeen.Store(true)
				r.lines, r.line = nil, nil
				return
			}
			r.lines = keepTail(append(append(r.lines, r.line...), '\n'))
			r.line = r.line[:0]
		case '\r':
			// Lines end in CR LF; the CR carries nothing worth reporting.
		default:
			if len(r.line) < maxPreVersionText {
				r.line = append(r.line, char)
			}
		}
	}
}

// keepTail drops the start of buf so that at most maxPreVersionText bytes remain.
func keepTail(buf []byte) []byte {
	if len(buf) <= maxPreVersionText {
		return buf
	}
	return append(buf[:0], buf[len(buf)-maxPreVersionText:]...)
}

// text returns the recorded pre-version text, at most maxPreVersionText bytes.
func (r *preVersionRecorder) text() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.versionSeen.Load() {
		return ""
	}
	text := append(append([]byte(nil), r.lines...), r.line...)
	text = bytes.TrimSuffix(keepTail(text), []byte{'\n'})
	return string(text)
}

// annotate adds the text the server sent to a handshake error, but only when the
// handshake failed before the server's version line arrived. Past that point the
// text explains nothing about the failure. Annotate an error after it has been
// classified, so that server-controlled text cannot change the classification.
func (r *preVersionRecorder) annotate(err error) error {
	text := r.text()
	if text == "" {
		return err
	}
	return fmt.Errorf("%w (server sent %q before any SSH version line)", err, text)
}
