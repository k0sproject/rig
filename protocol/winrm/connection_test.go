package winrm

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/k0sproject/rig/v2/protocol"
)

func Test_isAuthError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "http error 401",
			err:  fmt.Errorf("http error 401: Unauthorized"),
			want: true,
		},
		{
			name: "http error 403",
			err:  fmt.Errorf("http error 403: Forbidden"),
			want: true,
		},
		{
			name: "http response error 401",
			err:  fmt.Errorf("http response error: 401 - %w", errors.New("unauthorized")),
			want: true,
		},
		{
			name: "http response error 403",
			err:  fmt.Errorf("http response error: 403 - %w", errors.New("forbidden")),
			want: true,
		},
		{
			name: "connection refused",
			err:  fmt.Errorf("dial tcp 10.0.0.1:5985: connect: connection refused"),
			want: false,
		},
		{
			name: "http error 500",
			err:  fmt.Errorf("http error 500: Internal Server Error"),
			want: false,
		},
		{
			name: "create shell error wrapping auth",
			err:  fmt.Errorf("create shell: http error 401: Unauthorized"),
			want: true,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "timeout",
			err:  fmt.Errorf("context deadline exceeded"),
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isAuthError(tt.err)
			if got != tt.want {
				t.Errorf("isAuthError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// Test_authErrorWrapping verifies that the classification/wrapping logic used in
// probe produces ErrNonRetryable for auth errors and leaves other errors retryable.
// probe itself is not called here because it requires a live WinRM client; see the
// todo item for WinRM integration tests.
func Test_authErrorWrapping(t *testing.T) {
	authErr := fmt.Errorf("create shell: http error 401: Unauthorized")
	nonAuthErr := fmt.Errorf("dial tcp 10.0.0.1:5985: connect: connection refused")

	tests := []struct {
		name           string
		startErr       error
		wantNonRetry   bool
		wantErrContain string
	}{
		{
			name:           "auth failure becomes ErrNonRetryable",
			startErr:       authErr,
			wantNonRetry:   true,
			wantErrContain: authErr.Error(),
		},
		{
			name:           "network failure stays retryable",
			startErr:       nonAuthErr,
			wantNonRetry:   false,
			wantErrContain: nonAuthErr.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Exercise the classification logic directly rather than through the full
			// probe method (which requires a real WinRM client). Integration coverage
			// against a real WinRM server is deferred to the todo item for WinRM tests.
			var err error
			if isAuthError(tt.startErr) {
				err = fmt.Errorf("%w: %w", protocol.ErrNonRetryable, tt.startErr)
			} else {
				err = tt.startErr
			}

			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			gotNonRetry := errors.Is(err, protocol.ErrNonRetryable)
			if gotNonRetry != tt.wantNonRetry {
				t.Errorf("errors.Is(err, ErrNonRetryable) = %v, want %v (err: %v)", gotNonRetry, tt.wantNonRetry, err)
			}
			if tt.wantErrContain != "" {
				if msg := err.Error(); !strings.Contains(msg, tt.wantErrContain) {
					t.Errorf("err.Error() = %q, want it to contain %q", msg, tt.wantErrContain)
				}
			}
		})
	}
}

func TestConnect_probeClassification(t *testing.T) {
	tests := []struct {
		name         string
		statusCode   int
		wantNonRetry bool
	}{
		{
			name:         "401 becomes ErrNonRetryable",
			statusCode:   http.StatusUnauthorized,
			wantNonRetry: true,
		},
		{
			name:         "403 becomes ErrNonRetryable",
			statusCode:   http.StatusForbidden,
			wantNonRetry: true,
		},
		{
			name:         "500 stays retryable",
			statusCode:   http.StatusInternalServerError,
			wantNonRetry: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			t.Cleanup(srv.Close)

			// Parse host and port from the test server URL.
			u, parseErr := url.Parse(srv.URL)
			if parseErr != nil {
				t.Fatalf("url.Parse(%q) error = %v", srv.URL, parseErr)
			}
			host, portStr, splitErr := net.SplitHostPort(u.Host)
			if splitErr != nil {
				t.Fatalf("net.SplitHostPort(%q) error = %v", u.Host, splitErr)
			}
			port, convErr := strconv.Atoi(portStr)
			if convErr != nil {
				t.Fatalf("strconv.Atoi(%q) error = %v", portStr, convErr)
			}

			conn, err := NewConnection(Config{
				Address:  host,
				Port:     port,
				User:     "user",
				Password: "pass",
			})
			if err != nil {
				t.Fatalf("NewConnection() error = %v", err)
			}

			err = conn.Connect(context.Background())
			if err == nil {
				t.Fatal("Connect() succeeded against stub server, want error")
			}

			gotNonRetry := errors.Is(err, protocol.ErrNonRetryable)
			if gotNonRetry != tt.wantNonRetry {
				t.Errorf("Connect() errors.Is(err, ErrNonRetryable) = %v, want %v (err: %v)", gotNonRetry, tt.wantNonRetry, err)
			}

			if conn.IsConnected() {
				t.Error("IsConnected() = true after failed Connect, want false")
			}
		})
	}
}
