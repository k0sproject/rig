package remotefs

import (
	"errors"
	"io"
	"net"
	"strings"
)

// isTransportClosed reports whether err is a known session-teardown signal
// rather than a logical command failure. It is used when issuing a reboot:
// the OS may tear down the SSH/WinRM session as the shutdown begins, and
// that disconnection must be treated as success rather than an error.
//
// The matched strings are specific teardown phrases observed in SSH and WinRM
// transport implementations to avoid false positives such as "connection
// refused" or DNS failures, which would mask a reboot that never started.
func isTransportClosed(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection reset by peer") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "use of closed network connection") ||
		strings.Contains(msg, "unexpected eof") ||
		strings.Contains(msg, "read: eof")
}
