package remotefs

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
)

// HTTPTransport is implemented by remote filesystems that can proxy HTTP requests
// through the remote host. Since RoundTrip matches the http.RoundTripper signature,
// any FS value satisfies http.RoundTripper and can be used directly as http.Client.Transport.
type HTTPTransport interface {
	DownloadURL(url, dst string) error
	RoundTrip(req *http.Request) (*http.Response, error)
}

// HTTPStatus issues a HEAD request via t and returns the HTTP status code.
func HTTPStatus(ctx context.Context, t HTTPTransport, url string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return 0, fmt.Errorf("http-status %s: %w", url, err)
	}
	resp, err := t.RoundTrip(req)
	if err != nil {
		return 0, fmt.Errorf("http-status %s: %w", url, err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode, nil
}

// parseRawHTTPResponse decodes a base64-encoded raw HTTP/1.1 response and parses it.
func parseRawHTTPResponse(encoded string, req *http.Request) (*http.Response, error) {
	cleaned := strings.NewReplacer("\r", "", "\n", "").Replace(strings.TrimSpace(encoded))
	raw, err := base64.StdEncoding.DecodeString(cleaned)
	if err != nil {
		return nil, fmt.Errorf("decode http response: %w", err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(raw)), req)
	if err != nil {
		return nil, fmt.Errorf("parse http response: %w", err)
	}
	return resp, nil
}
