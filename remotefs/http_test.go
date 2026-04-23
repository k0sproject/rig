package remotefs_test

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/k0sproject/rig/v2/remotefs"
	"github.com/k0sproject/rig/v2/rigtest"
	"github.com/stretchr/testify/require"
)

// decodePSScript extracts and decodes the PowerShell script from a command using the -E encoded flag.
// PS encoding: each ASCII byte is stored as byte + \x00 (pseudo-UTF-16LE), then base64 encoded.
func decodePSScript(psCmd string) string {
	const marker = " -E "
	idx := strings.Index(psCmd, marker)
	if idx < 0 {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(psCmd[idx+len(marker):])
	if err != nil {
		return ""
	}
	var sb strings.Builder
	for i := 0; i+1 < len(raw); i += 2 {
		sb.WriteByte(raw[i])
	}
	return sb.String()
}

func TestPosixRoundTripGET(t *testing.T) {
	rawResp := "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\n\r\nhello"
	encoded := base64.StdEncoding.EncodeToString([]byte(rawResp))

	mr := rigtest.NewMockRunner()
	mr.AddCommandOutput(rigtest.Equal("command -v curl"), "/usr/bin/curl")
	mr.AddCommandOutput(rigtest.Contains("--http1.1"), encoded)
	f := remotefs.NewPosixFS(mr)

	req, err := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	require.NoError(t, err)

	resp, err := f.RoundTrip(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, 200, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "hello", string(body))
}

func TestPosixRoundTrip404(t *testing.T) {
	rawResp := "HTTP/1.1 404 Not Found\r\n\r\n"
	encoded := base64.StdEncoding.EncodeToString([]byte(rawResp))

	mr := rigtest.NewMockRunner()
	mr.AddCommandOutput(rigtest.Equal("command -v curl"), "/usr/bin/curl")
	mr.AddCommandOutput(rigtest.Contains("--http1.1"), encoded)
	f := remotefs.NewPosixFS(mr)

	req, err := http.NewRequest(http.MethodGet, "http://example.com/missing", nil)
	require.NoError(t, err)

	resp, err := f.RoundTrip(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, 404, resp.StatusCode)
}

func TestPosixRoundTripWithRequestBody(t *testing.T) {
	rawResp := "HTTP/1.1 201 Created\r\n\r\n"
	encoded := base64.StdEncoding.EncodeToString([]byte(rawResp))

	mr := rigtest.NewMockRunner()
	mr.AddCommandOutput(rigtest.Equal("command -v curl"), "/usr/bin/curl")
	mr.AddCommandOutput(rigtest.Contains("--http1.1"), encoded)
	f := remotefs.NewPosixFS(mr)

	req, err := http.NewRequest(http.MethodPost, "http://example.com/api", strings.NewReader(`{"key":"val"}`))
	require.NoError(t, err)

	resp, err := f.RoundTrip(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, 201, resp.StatusCode)
	require.Contains(t, mr.LastCommand(), "--data-binary")
}

func TestPosixRoundTripCurlUnavailable(t *testing.T) {
	mr := rigtest.NewMockRunner()
	mr.AddCommandFailure(rigtest.Equal("command -v curl"), errors.New("not found"))
	f := remotefs.NewPosixFS(mr)

	req, err := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	require.NoError(t, err)

	_, err = f.RoundTrip(req)
	require.Error(t, err)
}

func TestHTTPStatusFreeFuncPosix(t *testing.T) {
	rawResp := "HTTP/1.1 200 OK\r\n\r\n"
	encoded := base64.StdEncoding.EncodeToString([]byte(rawResp))

	mr := rigtest.NewMockRunner()
	mr.AddCommandOutput(rigtest.Equal("command -v curl"), "/usr/bin/curl")
	mr.AddCommandOutput(rigtest.Contains("--http1.1"), encoded)
	f := remotefs.NewPosixFS(mr)

	code, err := remotefs.HTTPStatus(context.Background(), f, "http://example.com/health")
	require.NoError(t, err)
	require.Equal(t, 200, code)

	require.Contains(t, mr.LastCommand(), "-I")
}

func TestWinRoundTripGET(t *testing.T) {
	rawResp := "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\n\r\nhello"
	encoded := base64.StdEncoding.EncodeToString([]byte(rawResp))

	mr := rigtest.NewMockRunner()
	mr.Windows = true
	mr.AddCommandOutput(rigtest.HasPrefix("powershell.exe"), encoded)
	f := remotefs.NewWindowsFS(mr)

	req, err := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	require.NoError(t, err)

	resp, err := f.RoundTrip(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, 200, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "hello", string(body))
}

func TestWinRoundTripCommandError(t *testing.T) {
	mr := rigtest.NewMockRunner()
	mr.Windows = true
	mr.AddCommandFailure(rigtest.HasPrefix("powershell.exe"), errors.New("exit 1"))
	f := remotefs.NewWindowsFS(mr)

	req, err := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	require.NoError(t, err)

	_, err = f.RoundTrip(req)
	require.Error(t, err)
}

func TestWinRoundTrip404(t *testing.T) {
	rawResp := "HTTP/1.1 404 Not Found\r\n\r\n"
	encoded := base64.StdEncoding.EncodeToString([]byte(rawResp))

	mr := rigtest.NewMockRunner()
	mr.Windows = true
	mr.AddCommandOutput(rigtest.HasPrefix("powershell.exe"), encoded)
	f := remotefs.NewWindowsFS(mr)

	req, err := http.NewRequest(http.MethodGet, "http://example.com/missing", nil)
	require.NoError(t, err)

	resp, err := f.RoundTrip(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, 404, resp.StatusCode)
}

func TestWinRoundTripWithRequestBody(t *testing.T) {
	rawResp := "HTTP/1.1 201 Created\r\n\r\n"
	encoded := base64.StdEncoding.EncodeToString([]byte(rawResp))

	mr := rigtest.NewMockRunner()
	mr.Windows = true
	mr.AddCommandOutput(rigtest.HasPrefix("powershell.exe"), encoded)
	f := remotefs.NewWindowsFS(mr)

	req, err := http.NewRequest(http.MethodPost, "http://example.com/api", strings.NewReader(`{"key":"val"}`))
	require.NoError(t, err)

	resp, err := f.RoundTrip(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, 201, resp.StatusCode)
	require.Contains(t, decodePSScript(mr.LastCommand()), "FromBase64String")
}
