package ssh

import (
	"errors"
	"io"
	"net"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var errHandshake = errors.New("handshake failed")

// recordFrom feeds data through a preVersionRecorder the way x/crypto reads a
// version string, one byte per Read, and returns the recorder.
func recordFrom(t *testing.T, data string) *preVersionRecorder {
	t.Helper()

	client, server := net.Pipe()
	t.Cleanup(func() { client.Close() })
	go func() {
		defer server.Close()
		_, _ = server.Write([]byte(data))
	}()

	recorder := newPreVersionRecorder(client)
	buf := make([]byte, 1)
	for {
		if _, err := recorder.Read(buf); err != nil {
			require.ErrorIs(t, err, io.EOF)
			return recorder
		}
	}
}

func TestPreVersionRecorderAnnotatesTextBeforeVersion(t *testing.T) {
	recorder := recordFrom(t, "Not allowed at this time\r\n")

	err := recorder.annotate(errHandshake)
	require.ErrorIs(t, err, errHandshake)
	require.ErrorContains(t, err, `"Not allowed at this time"`)
}

func TestPreVersionRecorderKeepsEveryLineAndUnterminatedTail(t *testing.T) {
	recorder := recordFrom(t, "first\nsecond\r\npartial")

	require.Equal(t, "first\nsecond\npartial", recorder.text())
}

func TestPreVersionRecorderIgnoresTextOnceVersionArrives(t *testing.T) {
	recorder := recordFrom(t, "notice\r\nSSH-2.0-OpenSSH_9.9\r\nafter version\n")

	require.Empty(t, recorder.text())
	require.Equal(t, errHandshake, recorder.annotate(errHandshake),
		"a failure after the version line is not explained by pre-version text")
}

func TestPreVersionRecorderLeavesErrorAloneWithoutText(t *testing.T) {
	recorder := recordFrom(t, "")

	require.Equal(t, errHandshake, recorder.annotate(errHandshake))
}

func TestPreVersionRecorderQuotesControlCharacters(t *testing.T) {
	recorder := recordFrom(t, "\x1b[2Jcleared\r\n")

	err := recorder.annotate(errHandshake)
	require.NotContains(t, err.Error(), "\x1b", "server text must not reach a terminal unescaped")
	require.ErrorContains(t, err, `\x1b[2Jcleared`)
}

func TestPreVersionRecorderBoundsTextAcrossLines(t *testing.T) {
	recorder := recordFrom(t, strings.Repeat("a", maxPreVersionText-1)+"\n"+strings.Repeat("b", 2*maxPreVersionText))

	require.LessOrEqual(t, len(recorder.text()), maxPreVersionText)
}

func TestPreVersionRecorderKeepsTheLastLine(t *testing.T) {
	recorder := recordFrom(t, strings.Repeat("motd line\r\n", 100)+"Not allowed at this time\r\n")

	text := recorder.text()
	require.LessOrEqual(t, len(text), maxPreVersionText)
	require.True(t, strings.HasSuffix(text, "Not allowed at this time"), "the refusal is the line worth keeping, got %q", text)
}
