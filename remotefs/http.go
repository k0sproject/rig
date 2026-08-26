package remotefs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ErrHTTPStatusNotSupported is returned by HTTPStatusInsecure when the FS
// implementation does not support HTTP status checks. Callers can detect this
// with errors.Is.
var ErrHTTPStatusNotSupported = errors.New("http status check not supported by this filesystem type")

var (
	errURLInvalidCharacter    = errors.New("url contains invalid character")
	errURLContainsCredentials = errors.New("url must not contain credentials")
	errURLInvalidScheme       = errors.New("url scheme must be http or https")
	errURLMissingHost         = errors.New("url must contain a host")
)

// httpStatusProvider is implemented by FS types that support insecure HTTP status checks.
type httpStatusProvider interface {
	httpStatusInsecure(ctx context.Context, url string) (int, error)
}

func validateHTTPURL(rawURL string) error {
	for _, c := range rawURL {
		if c < 0x20 || c == 0x7f {
			return fmt.Errorf("%w: %q", errURLInvalidCharacter, c)
		}
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if scheme := strings.ToLower(u.Scheme); scheme != "http" && scheme != "https" {
		return fmt.Errorf("%w: %q", errURLInvalidScheme, u.Scheme)
	}
	if u.Host == "" {
		return errURLMissingHost
	}
	if u.User != nil {
		return errURLContainsCredentials
	}
	return nil
}

// HTTPStatusInsecure checks whether url is reachable and returns the HTTP status
// code, skipping TLS certificate verification. On Windows with PowerShell 5.x,
// TLS certificate verification is not skipped and requests will fail for
// self-signed certificates.
func HTTPStatusInsecure(ctx context.Context, fs FS, url string) (int, error) {
	if err := validateHTTPURL(url); err != nil {
		return 0, fmt.Errorf("HTTPStatusInsecure: %w", err)
	}
	p, ok := fs.(httpStatusProvider)
	if !ok {
		return 0, ErrHTTPStatusNotSupported
	}
	return p.httpStatusInsecure(ctx, url)
}

// ErrHTTPHeadNotSupported is returned by [HTTPHead] when the FS implementation
// or the remote host cannot perform HTTP HEAD requests. Callers can detect this
// with errors.Is.
var ErrHTTPHeadNotSupported = errors.New("http head not supported by this filesystem type")

// errHTTPHeadNoStatus is returned when the output of the remote tool contained
// no recognizable HTTP status line.
var errHTTPHeadNoStatus = errors.New("could not determine http status from head response")

// URLInfo describes what an HTTP HEAD request reported about a URL. It carries
// the values that are useful for deciding whether a previously downloaded file
// is still current.
type URLInfo struct {
	// StatusCode is the status of the final response, after any redirects.
	StatusCode int

	// ContentLength is the Content-Length of the final response, or -1 when the
	// server did not report one, which happens under chunked transfer encoding.
	ContentLength int64

	// ETag is the entity tag exactly as the server sent it, including the quotes
	// and any weak validator prefix, or empty when the server sent none. It is
	// opaque: compare it for equality with a previously recorded value, never
	// try to derive a content hash from it.
	ETag string

	// LastModified is the parsed Last-Modified header, or the zero time when the
	// server sent none or it could not be parsed.
	LastModified time.Time

	// AcceptRanges reports whether the server advertised support for range
	// requests.
	AcceptRanges bool
}

// httpHeadProvider is implemented by FS types that can perform HTTP HEAD
// requests on the remote host.
type httpHeadProvider interface {
	httpHead(ctx context.Context, url string) (*URLInfo, error)
}

// [HTTPHead] reaches the implementations through a type assertion, so a drifting
// signature would silently turn into ErrHTTPHeadNotSupported rather than a
// compile error. These keep that honest.
var (
	_ httpHeadProvider = (*PosixFS)(nil)
	_ httpHeadProvider = (*WinFS)(nil)
)

// HTTPHead performs an HTTP HEAD request for url from the remote host and
// reports what the server said about it. Unlike [HTTPStatusInsecure], TLS
// certificates are verified.
//
// The request is made from the remote host rather than from the machine running
// rig, so the result reflects what that host can actually reach. This matters
// when the two do not share a view of the network, as with an internal mirror.
//
// A non-2xx StatusCode is returned without an error; inspecting it is left to
// the caller. Servers that reject HEAD typically answer 405, which is reported
// the same way.
func HTTPHead(ctx context.Context, fs FS, url string) (*URLInfo, error) {
	if err := validateHTTPURL(url); err != nil {
		return nil, fmt.Errorf("HTTPHead: %w", err)
	}
	p, ok := fs.(httpHeadProvider)
	if !ok {
		return nil, ErrHTTPHeadNotSupported
	}
	return p.httpHead(ctx, url)
}

// applyHeader records a single response header into info, ignoring any header
// that is not one of the ones URLInfo carries.
func applyHeader(info *URLInfo, key, value string) {
	switch strings.ToLower(key) {
	case "content-length":
		if n, err := strconv.ParseInt(value, 10, 64); err == nil && n >= 0 {
			info.ContentLength = n
		}
	case "etag":
		info.ETag = value
	case "last-modified":
		if t, err := http.ParseTime(value); err == nil {
			info.LastModified = t
		}
	case "accept-ranges":
		info.AcceptRanges = value != "" && !strings.EqualFold(value, "none")
	}
}

// parseHeadResponse builds a URLInfo from raw HTTP response headers.
//
// Following redirects makes the tools emit one header block per response, and
// the earlier blocks carry values that must not leak into the result -- a 302
// commonly has its own Content-Length of 0. Every status line therefore starts
// a fresh URLInfo, leaving the last response as the one that is returned.
//
// The input is accepted in the shapes curl and wget produce: CRLF line endings,
// leading whitespace, and header names in any case.
func parseHeadResponse(raw string) (*URLInfo, error) {
	var info *URLInfo
	for line := range strings.SplitSeq(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "HTTP/") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			code, err := strconv.Atoi(fields[1])
			if err != nil {
				continue
			}
			info = &URLInfo{StatusCode: code, ContentLength: -1}
			continue
		}
		if info == nil {
			// A header before any status line has no response to belong to.
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		applyHeader(info, strings.TrimSpace(key), strings.TrimSpace(value))
	}
	if info == nil {
		return nil, errHTTPHeadNoStatus
	}
	return info, nil
}
