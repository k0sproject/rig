package remotefs

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const maxRawResponseSize = 32 * 1024 * 1024 // 32 MiB encoded (~24 MiB decoded)

var (
	errNilRequest        = errors.New("net/http: nil Request")
	errNilRequestURL     = errors.New("net/http: nil Request.URL")
	errUnsupportedScheme = errors.New("unsupported URL scheme")
	errMissingURLHost    = errors.New("missing URL host")
	errUserinfoInURL     = errors.New("URL must not contain userinfo (credentials in URLs leak to remote command line)")
	errInvalidURLChars   = errors.New("URL contains control characters (CR, LF, or NUL)")
	errInvalidHeader     = errors.New("invalid header: contains CR, LF, or NUL")
	errResponseTooLarge  = errors.New("http response exceeds maximum size")
)

// validateRoundTripURL returns an error if u has an unsupported scheme, no host, userinfo, or control characters.
// Must be called after a nil-URL guard.
func validateRoundTripURL(target *url.URL) error {
	if strings.ContainsAny(target.String(), "\r\n\x00") {
		return errInvalidURLChars
	}
	switch strings.ToLower(target.Scheme) {
	case "http", "https":
	default:
		return fmt.Errorf("%w: %q", errUnsupportedScheme, target.Scheme)
	}
	if target.Host == "" {
		return errMissingURLHost
	}
	if target.User != nil {
		return errUserinfoInURL
	}
	return nil
}

// HTTPTransport is implemented by remote filesystems that can proxy HTTP requests
// through the remote host. Since RoundTrip matches the http.RoundTripper signature,
// any FS value satisfies http.RoundTripper and can be used directly as http.Client.Transport.
//
// Note: RoundTrip materializes the entire HTTP response on the remote side before
// transferring it to the caller as a base64-encoded string. Depending on the
// implementation, the remote side may buffer the response in memory or write it to a
// temporary file, and the caller also buffers the full response while decoding and
// parsing it. It is not suitable for large response bodies; use DownloadURL for
// downloading large files instead.
type HTTPTransport interface {
	DownloadURL(url, dst string) error
	RoundTrip(req *http.Request) (*http.Response, error)
}

// HTTPStatus issues a HEAD request via t and returns the HTTP status code.
func HTTPStatus(ctx context.Context, t http.RoundTripper, url string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return 0, fmt.Errorf("http-status: %w", err)
	}
	resp, err := t.RoundTrip(req)
	if err != nil {
		return 0, fmt.Errorf("http-status %s: %w", req.URL.Redacted(), err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode, nil
}

// parseRawHTTPResponse decodes a base64-encoded raw HTTP/1.1 response and parses it.
// 100 Continue responses are consumed and discarded; all other responses are returned as-is.
func parseRawHTTPResponse(encoded string, req *http.Request) (*http.Response, error) {
	cleaned := strings.NewReplacer("\r", "", "\n", "").Replace(strings.TrimSpace(encoded))
	if len(cleaned) > maxRawResponseSize {
		return nil, fmt.Errorf("%w (%d bytes)", errResponseTooLarge, len(cleaned))
	}
	raw, err := base64.StdEncoding.DecodeString(cleaned)
	if err != nil {
		return nil, fmt.Errorf("decode http response: %w", err)
	}
	reader := bufio.NewReader(bytes.NewReader(raw))
	for {
		resp, err := http.ReadResponse(reader, req)
		if err != nil {
			return nil, fmt.Errorf("parse http response: %w", err)
		}
		if resp.StatusCode != http.StatusContinue {
			return resp, nil
		}
		_ = resp.Body.Close()
	}
}
