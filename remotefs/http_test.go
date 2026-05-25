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

func TestPosixRoundTripGET(t *testing.T) {
	rawResp := "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\n\r\nhello"
	encoded := base64.StdEncoding.EncodeToString([]byte(rawResp))

	mr := rigtest.NewMockRunner()
	mr.AddCommandOutput(rigtest.Equal("command -v curl"), "/usr/bin/curl")
	mr.AddCommandOutput(rigtest.Equal("command -v base64"), "/usr/bin/base64")
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
	mr.AddCommandOutput(rigtest.Equal("command -v base64"), "/usr/bin/base64")
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
	mr.AddCommandOutput(rigtest.Equal("command -v base64"), "/usr/bin/base64")
	mr.AddCommandOutput(rigtest.Contains("--http1.1"), encoded)
	f := remotefs.NewPosixFS(mr)

	req, err := http.NewRequest(http.MethodPost, "http://example.com/api", strings.NewReader(`{"key":"val"}`))
	require.NoError(t, err)

	resp, err := f.RoundTrip(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, 201, resp.StatusCode)
	require.Contains(t, mr.LastCommand(), "--data-binary")
	// curl adds Content-Type: application/x-www-form-urlencoded for --data-binary when none is set;
	// we suppress it so callers get the same default-free behavior as net/http.
	require.Contains(t, mr.LastCommand(), "Content-Type:")
}

func TestPosixRoundTripBodyWithContentType(t *testing.T) {
	rawResp := "HTTP/1.1 200 OK\r\n\r\n"
	encoded := base64.StdEncoding.EncodeToString([]byte(rawResp))

	mr := rigtest.NewMockRunner()
	mr.AddCommandOutput(rigtest.Equal("command -v curl"), "/usr/bin/curl")
	mr.AddCommandOutput(rigtest.Equal("command -v base64"), "/usr/bin/base64")
	mr.AddCommandOutput(rigtest.Contains("--http1.1"), encoded)
	f := remotefs.NewPosixFS(mr)

	req, err := http.NewRequest(http.MethodPost, "http://example.com/api", strings.NewReader(`{"key":"val"}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := f.RoundTrip(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, 200, resp.StatusCode)
	require.Contains(t, mr.LastCommand(), "Content-Type: application/json")
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

func TestPosixRoundTripBase64Unavailable(t *testing.T) {
	mr := rigtest.NewMockRunner()
	mr.AddCommandOutput(rigtest.Equal("command -v curl"), "/usr/bin/curl")
	mr.AddCommandFailure(rigtest.Equal("command -v base64"), errors.New("not found"))
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
	mr.AddCommandOutput(rigtest.Equal("command -v base64"), "/usr/bin/base64")
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
	script, ok := decodePSScript(mr.LastCommand())
	require.True(t, ok, "expected encoded PS command")
	require.Contains(t, script, "OpenStandardInput")
}

func TestRoundTripURLValidation(t *testing.T) {
	tests := []struct {
		name string
		req  func() *http.Request
	}{
		{"unsupported scheme", func() *http.Request {
			req, _ := http.NewRequest(http.MethodGet, "ftp://example.com/", nil)
			return req
		}},
		{"missing host", func() *http.Request {
			req, _ := http.NewRequest(http.MethodGet, "http://example.com/", nil)
			req.URL.Host = ""
			return req
		}},
		{"userinfo in URL", func() *http.Request {
			req, _ := http.NewRequest(http.MethodGet, "http://user:pass@example.com/", nil)
			return req
		}},
		{"CR in URL host", func() *http.Request {
			req, _ := http.NewRequest(http.MethodGet, "http://example.com/", nil)
			req.URL.Host = "example.com\r"
			return req
		}},
		{"LF in URL host", func() *http.Request {
			req, _ := http.NewRequest(http.MethodGet, "http://example.com/", nil)
			req.URL.Host = "example.com\n"
			return req
		}},
		{"NUL in URL host", func() *http.Request {
			req, _ := http.NewRequest(http.MethodGet, "http://example.com/", nil)
			req.URL.Host = "example.com\x00"
			return req
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Posix: validateRoundTripURL runs before requireHTTPTools, so no tool stubs needed.
			mr := rigtest.NewMockRunner()
			f := remotefs.NewPosixFS(mr)
			_, err := f.RoundTrip(tc.req())
			require.Error(t, err)
		})
	}
}

func TestCurlHeaderSanitization(t *testing.T) {
	okResp := base64.StdEncoding.EncodeToString([]byte("HTTP/1.1 200 OK\r\n\r\n"))

	stubTools := func(mr *rigtest.MockRunner) {
		mr.AddCommandOutput(rigtest.Equal("command -v curl"), "/usr/bin/curl")
		mr.AddCommandOutput(rigtest.Equal("command -v base64"), "/usr/bin/base64")
	}

	t.Run("CR in header name rejected", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		stubTools(mr)
		f := remotefs.NewPosixFS(mr)
		req, _ := http.NewRequest(http.MethodGet, "http://example.com/", nil)
		req.Header["X-Bad\rName"] = []string{"value"}
		_, err := f.RoundTrip(req)
		require.Error(t, err)
	})

	t.Run("LF in header value rejected", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		stubTools(mr)
		f := remotefs.NewPosixFS(mr)
		req, _ := http.NewRequest(http.MethodGet, "http://example.com/", nil)
		req.Header["X-Custom"] = []string{"val\nue"}
		_, err := f.RoundTrip(req)
		require.Error(t, err)
	})

	t.Run("NUL in header value rejected", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		stubTools(mr)
		f := remotefs.NewPosixFS(mr)
		req, _ := http.NewRequest(http.MethodGet, "http://example.com/", nil)
		req.Header["X-Custom"] = []string{"val\x00ue"}
		_, err := f.RoundTrip(req)
		require.Error(t, err)
	})

	t.Run("req.Host injected as Host header", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		stubTools(mr)
		mr.AddCommandOutput(rigtest.Contains("--http1.1"), okResp)
		f := remotefs.NewPosixFS(mr)
		req, _ := http.NewRequest(http.MethodGet, "http://example.com/", nil)
		req.Host = "override.example.com"
		resp, err := f.RoundTrip(req)
		require.NoError(t, err)
		_ = resp.Body.Close()
		require.Contains(t, mr.LastCommand(), "Host: override.example.com")
	})

	t.Run("Host in req.Header is skipped", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		stubTools(mr)
		mr.AddCommandOutput(rigtest.Contains("--http1.1"), okResp)
		f := remotefs.NewPosixFS(mr)
		req, _ := http.NewRequest(http.MethodGet, "http://example.com/", nil)
		req.Header.Set("Host", "should-not-appear.example.com")
		resp, err := f.RoundTrip(req)
		require.NoError(t, err)
		_ = resp.Body.Close()
		require.NotContains(t, mr.LastCommand(), "should-not-appear.example.com")
	})

	t.Run("Cookie values joined with semicolon", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		stubTools(mr)
		mr.AddCommandOutput(rigtest.Contains("--http1.1"), okResp)
		f := remotefs.NewPosixFS(mr)
		req, _ := http.NewRequest(http.MethodGet, "http://example.com/", nil)
		req.Header["Cookie"] = []string{"a=1", "b=2"}
		resp, err := f.RoundTrip(req)
		require.NoError(t, err)
		_ = resp.Body.Close()
		require.Contains(t, mr.LastCommand(), "Cookie: a=1; b=2")
	})
}
