package remotefs_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/k0sproject/rig/v2/remotefs"
	"github.com/k0sproject/rig/v2/rigtest"
	"github.com/stretchr/testify/require"
)

// Test URLs use the .invalid TLD, which RFC 2606 reserves and guarantees will
// never resolve. Nothing here reaches the network -- every FS is backed by a
// rigtest.MockRunner, which records command strings instead of running them --
// and an unresolvable host means that stays true even if a test is later
// rewired by mistake.

func TestHTTPStatusInsecureURLValidation(t *testing.T) {
	mr := rigtest.NewMockRunner()
	f := remotefs.NewPosixFS(mr)

	for _, rawURL := range []string{
		"file:///etc/passwd",
		"ftp://test.invalid",
		"http:///path",
		"/relative/path",
		"http://user:pass@test.invalid",
		"http://test.invalid\x00",
	} {
		_, err := remotefs.HTTPStatusInsecure(context.Background(), f, rawURL)
		require.Error(t, err, "expected error for %q", rawURL)
	}
}

func TestPosixHTTPStatusInsecure(t *testing.T) {
	t.Run("200", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandOutput(rigtest.HasPrefix("curl"), "200")
		f := remotefs.NewPosixFS(mr)
		code, err := remotefs.HTTPStatusInsecure(context.Background(), f, "https://test.invalid/health")
		require.NoError(t, err)
		require.Equal(t, 200, code)
		require.Contains(t, mr.LastCommand(), "-k")
	})
	t.Run("503", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandOutput(rigtest.HasPrefix("curl"), "503")
		f := remotefs.NewPosixFS(mr)
		code, err := remotefs.HTTPStatusInsecure(context.Background(), f, "https://test.invalid/health")
		require.NoError(t, err)
		require.Equal(t, 503, code)
	})
	t.Run("curl error", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandFailure(rigtest.HasPrefix("curl"), errors.New("exit status 60"))
		f := remotefs.NewPosixFS(mr)
		_, err := remotefs.HTTPStatusInsecure(context.Background(), f, "https://test.invalid/health")
		require.Error(t, err)
	})
}

func TestPosixHTTPStatusInsecureWget(t *testing.T) {
	noCurl := errors.New("not found")

	t.Run("200 via wget", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandFailure(rigtest.Equal("command -v curl"), noCurl)
		mr.AddCommandOutput(rigtest.Equal("command -v wget"), "/usr/bin/wget")
		mr.AddCommand(rigtest.HasPrefix("wget"), func(a *rigtest.A) error {
			_, err := fmt.Fprint(a.Stderr, "  HTTP/1.1 200 OK\n")
			return err
		})
		f := remotefs.NewPosixFS(mr)
		code, err := remotefs.HTTPStatusInsecure(context.Background(), f, "https://test.invalid/health")
		require.NoError(t, err)
		require.Equal(t, 200, code)
	})

	t.Run("301 via wget", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandFailure(rigtest.Equal("command -v curl"), noCurl)
		mr.AddCommandOutput(rigtest.Equal("command -v wget"), "/usr/bin/wget")
		mr.AddCommand(rigtest.HasPrefix("wget"), func(a *rigtest.A) error {
			_, err := fmt.Fprint(a.Stderr, "  HTTP/1.1 301 Moved Permanently\n")
			return err
		})
		f := remotefs.NewPosixFS(mr)
		code, err := remotefs.HTTPStatusInsecure(context.Background(), f, "https://test.invalid/health")
		require.NoError(t, err)
		require.Equal(t, 301, code)
	})

	t.Run("no tools", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandFailure(rigtest.Equal("command -v curl"), noCurl)
		mr.AddCommandFailure(rigtest.Equal("command -v wget"), noCurl)
		f := remotefs.NewPosixFS(mr)
		_, err := remotefs.HTTPStatusInsecure(context.Background(), f, "https://test.invalid/health")
		require.ErrorIs(t, err, remotefs.ErrHTTPStatusNotSupported)
	})
}

func TestWindowsHTTPStatusInsecure(t *testing.T) {
	t.Run("200", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandOutput(rigtest.HasPrefix("powershell.exe"), "200")
		f := remotefs.NewWindowsFS(mr)
		code, err := remotefs.HTTPStatusInsecure(context.Background(), f, "https://test.invalid/health")
		require.NoError(t, err)
		require.Equal(t, 200, code)
	})
	t.Run("failure", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandFailure(rigtest.HasPrefix("powershell.exe"), errors.New("exit 1"))
		f := remotefs.NewWindowsFS(mr)
		_, err := remotefs.HTTPStatusInsecure(context.Background(), f, "https://test.invalid/health")
		require.Error(t, err)
	})
}

// curlHeadOutput is the shape curl -sS -L -I produces for a redirecting URL,
// captured from a real GitHub release asset. The 302 block carries its own
// content-length of 0, which must not leak into the reported result.
const curlHeadOutput = "HTTP/2 302 \r\n" +
	"content-type: text/html; charset=utf-8\r\n" +
	"location: https://release-assets.githubusercontent.com/xyz\r\n" +
	"content-length: 0\r\n" +
	"\r\n" +
	"HTTP/2 200 \r\n" +
	"last-modified: Mon, 30 Sep 2024 11:42:01 GMT\r\n" +
	"etag: \"0x8DCE144E5F2F047\"\r\n" +
	"accept-ranges: bytes\r\n" +
	"content-length: 260830864\r\n" +
	"\r\n"

// wgetHeadOutput is the shape wget --spider --server-response writes to stderr
// for the same URL: two-space indent, mixed-case header names.
const wgetHeadOutput = "  HTTP/1.1 302 Found\n" +
	"  Content-Length: 0\n" +
	"  Location: https://release-assets.githubusercontent.com/xyz\n" +
	"\n" +
	"  HTTP/1.1 200 OK\n" +
	"  Content-Length: 260830864\n" +
	"  Last-Modified: Mon, 30 Sep 2024 11:42:01 GMT\n" +
	"  ETag: \"0x8DCE144E5F2F047\"\n" +
	"  Accept-Ranges: bytes\n"

func requireReleaseAsset(t *testing.T, info *remotefs.URLInfo) {
	t.Helper()
	require.Equal(t, 200, info.StatusCode)
	require.Equal(t, int64(260830864), info.ContentLength, "the redirect's content-length must not leak")
	require.Equal(t, `"0x8DCE144E5F2F047"`, info.ETag)
	require.True(t, info.AcceptRanges)
	require.Equal(t, time.Date(2024, 9, 30, 11, 42, 1, 0, time.UTC), info.LastModified.UTC())
}

func TestHTTPHeadURLValidation(t *testing.T) {
	mr := rigtest.NewMockRunner()
	f := remotefs.NewPosixFS(mr)

	for _, rawURL := range []string{
		"file:///etc/passwd",
		"ftp://test.invalid",
		"http:///path",
		"/relative/path",
		"http://user:pass@test.invalid",
		"http://test.invalid\x00",
	} {
		_, err := remotefs.HTTPHead(context.Background(), f, rawURL)
		require.Error(t, err, "expected error for %q", rawURL)
	}

	// The loop above only asserts that an error came back. Pin the
	// security-relevant branch so it cannot start failing for some unrelated
	// reason while the credentials check silently goes away.
	_, err := remotefs.HTTPHead(context.Background(), f, "http://user:pass@test.invalid")
	require.ErrorContains(t, err, "credentials")
}

func TestPosixHTTPHead(t *testing.T) {
	t.Run("curl", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandOutput(rigtest.HasPrefix("curl"), curlHeadOutput)
		f := remotefs.NewPosixFS(mr)

		info, err := remotefs.HTTPHead(context.Background(), f, "https://test.invalid/a.tar")
		require.NoError(t, err)
		requireReleaseAsset(t, info)
		require.NotContains(t, mr.LastCommand(), " -k", "HTTPHead must verify TLS certificates")
	})

	t.Run("wget fallback", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandFailure(rigtest.Equal("command -v curl"), errors.New("not found"))
		mr.AddCommandOutput(rigtest.Equal("command -v wget"), "/usr/bin/wget")
		mr.AddCommand(rigtest.HasPrefix("wget"), func(a *rigtest.A) error {
			_, err := fmt.Fprint(a.Stderr, wgetHeadOutput)
			return err
		})
		f := remotefs.NewPosixFS(mr)

		info, err := remotefs.HTTPHead(context.Background(), f, "https://test.invalid/a.tar")
		require.NoError(t, err)
		requireReleaseAsset(t, info)
	})

	t.Run("wget reports headers even when it exits non-zero", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandFailure(rigtest.Equal("command -v curl"), errors.New("not found"))
		mr.AddCommandOutput(rigtest.Equal("command -v wget"), "/usr/bin/wget")
		mr.AddCommand(rigtest.HasPrefix("wget"), func(a *rigtest.A) error {
			if _, err := fmt.Fprint(a.Stderr, "  HTTP/1.1 404 Not Found\n"); err != nil {
				return err
			}
			return errors.New("exit status 8")
		})
		f := remotefs.NewPosixFS(mr)

		info, err := remotefs.HTTPHead(context.Background(), f, "https://test.invalid/a.tar")
		require.NoError(t, err)
		require.Equal(t, 404, info.StatusCode)
	})

	t.Run("non-2xx is not an error", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandOutput(rigtest.HasPrefix("curl"), "HTTP/2 405 \r\n\r\n")
		f := remotefs.NewPosixFS(mr)

		info, err := remotefs.HTTPHead(context.Background(), f, "https://test.invalid/a.tar")
		require.NoError(t, err)
		require.Equal(t, 405, info.StatusCode)
	})

	t.Run("missing content-length reports -1", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandOutput(rigtest.HasPrefix("curl"), "HTTP/2 200 \r\ntransfer-encoding: chunked\r\n\r\n")
		f := remotefs.NewPosixFS(mr)

		info, err := remotefs.HTTPHead(context.Background(), f, "https://test.invalid/a.tar")
		require.NoError(t, err)
		require.Equal(t, int64(-1), info.ContentLength)
		require.Empty(t, info.ETag)
		require.True(t, info.LastModified.IsZero())
		require.False(t, info.AcceptRanges)
	})

	t.Run("accept-ranges none", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandOutput(rigtest.HasPrefix("curl"), "HTTP/2 200 \r\naccept-ranges: none\r\n\r\n")
		f := remotefs.NewPosixFS(mr)

		info, err := remotefs.HTTPHead(context.Background(), f, "https://test.invalid/a.tar")
		require.NoError(t, err)
		require.False(t, info.AcceptRanges)
	})

	t.Run("unparseable output", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandOutput(rigtest.HasPrefix("curl"), "something went sideways")
		f := remotefs.NewPosixFS(mr)

		_, err := remotefs.HTTPHead(context.Background(), f, "https://test.invalid/a.tar")
		require.Error(t, err)
	})

	t.Run("curl error", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandFailure(rigtest.HasPrefix("curl"), errors.New("exit status 6"))
		f := remotefs.NewPosixFS(mr)

		_, err := remotefs.HTTPHead(context.Background(), f, "https://test.invalid/a.tar")
		require.Error(t, err)
	})

	t.Run("no tools", func(t *testing.T) {
		notFound := errors.New("not found")
		mr := rigtest.NewMockRunner()
		mr.AddCommandFailure(rigtest.Equal("command -v curl"), notFound)
		mr.AddCommandFailure(rigtest.Equal("command -v wget"), notFound)
		f := remotefs.NewPosixFS(mr)

		_, err := remotefs.HTTPHead(context.Background(), f, "https://test.invalid/a.tar")
		require.ErrorIs(t, err, remotefs.ErrHTTPHeadNotSupported)
	})
}

func TestWindowsHTTPHead(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandOutput(rigtest.HasPrefix("powershell.exe"),
			"HTTP/1.1 200\r\n"+
				"Content-Length: 260830864\r\n"+
				"Last-Modified: Mon, 30 Sep 2024 11:42:01 GMT\r\n"+
				"ETag: \"0x8DCE144E5F2F047\"\r\n"+
				"Accept-Ranges: bytes\r\n")
		f := remotefs.NewWindowsFS(mr)

		info, err := remotefs.HTTPHead(context.Background(), f, "https://test.invalid/a.tar")
		require.NoError(t, err)
		requireReleaseAsset(t, info)
	})

	t.Run("non-2xx is not an error", func(t *testing.T) {
		// Invoke-WebRequest reports non-2xx by throwing, and the script digs the
		// response back out of the exception. Windows must agree with the posix
		// paths that a status is a result, not a failure.
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandOutput(rigtest.HasPrefix("powershell.exe"),
			"HTTP/1.1 404\r\nContent-Length: 9\r\n")
		f := remotefs.NewWindowsFS(mr)

		info, err := remotefs.HTTPHead(context.Background(), f, "https://test.invalid/a.tar")
		require.NoError(t, err)
		require.Equal(t, 404, info.StatusCode)
		require.Equal(t, int64(9), info.ContentLength)
	})

	t.Run("transport failure is an error", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandFailure(rigtest.HasPrefix("powershell.exe"), errors.New("exit status 1"))
		f := remotefs.NewWindowsFS(mr)

		_, err := remotefs.HTTPHead(context.Background(), f, "https://test.invalid/a.tar")
		require.Error(t, err)
	})
}
