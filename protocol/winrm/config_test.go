package winrm

import (
	"context"
	"strings"
	"testing"

	"github.com/k0sproject/rig/v2/rigtest"
)

func Test_passwordExposure(t *testing.T) {
	const (
		cleartext  = "cleartext HTTP basic authentication"
		unverified = "certificate verification disabled"
	)

	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{
			name: "plain http with a password",
			cfg:  Config{User: "Administrator", Password: "hunter2"},
			want: cleartext,
		},
		{
			name: "plain http with ntlm encrypts at the message level",
			cfg:  Config{User: "Administrator", Password: "hunter2", UseNTLM: true},
			want: "",
		},
		{
			name: "https with a verified certificate",
			cfg:  Config{User: "Administrator", Password: "hunter2", UseHTTPS: true},
			want: "",
		},
		{
			name: "https with verification disabled",
			cfg:  Config{User: "Administrator", Password: "hunter2", UseHTTPS: true, Insecure: true},
			want: unverified,
		},
		{
			name: "ntlm does not redeem an unverified certificate",
			cfg:  Config{User: "Administrator", Password: "hunter2", UseHTTPS: true, Insecure: true, UseNTLM: true},
			want: unverified,
		},
		{
			name: "certificate authentication over an unverified tls connection",
			cfg:  Config{CertPath: "/c.pem", KeyPath: "/k.pem", UseHTTPS: true, Insecure: true},
			want: "",
		},
		{
			name: "certificate authentication over plain http",
			cfg:  Config{CertPath: "/c.pem", KeyPath: "/k.pem"},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.passwordExposure()
			switch {
			case tt.want == "" && got != "":
				t.Fatalf("expected no warning, got %q", got)
			case tt.want != "" && !strings.Contains(got, tt.want):
				t.Fatalf("expected a warning containing %q, got %q", tt.want, got)
			}
		})
	}
}

// connectExpectingFailure runs Connect against an already-cancelled context,
// which fails fast at the transport. The warning is emitted before any of
// that, so this exercises it without needing a reachable host.
func connectExpectingFailure(t *testing.T, cfg Config, logger *rigtest.MockLogger) *Connection {
	t.Helper()
	conn, err := NewConnection(cfg, WithLogger(logger))
	if err != nil {
		t.Fatalf("NewConnection() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := conn.Connect(ctx); err == nil {
		t.Fatal("Connect() succeeded against a cancelled context, want error")
	}
	return conn
}

func TestConnectWarnsAboutPasswordExposure(t *testing.T) {
	insecureTLS := Config{
		Address:  "10.0.0.1",
		User:     "Administrator",
		Password: "hunter2",
		UseHTTPS: true,
		Insecure: true,
	}

	t.Run("warns at warn level, naming the host", func(t *testing.T) {
		logger := &rigtest.MockLogger{}
		conn := connectExpectingFailure(t, insecureTLS, logger)

		msgs := logger.Messages()
		if len(msgs) != 1 {
			t.Fatalf("expected exactly 1 log message, got %d: %v", len(msgs), msgs)
		}
		if lvl := msgs[0].Level(); lvl != 2 {
			t.Errorf("expected warn level 2, got %d", lvl)
		}
		if !strings.Contains(msgs[0].Message(), "certificate verification disabled") {
			t.Errorf("unexpected message %q", msgs[0].Message())
		}
		if !strings.Contains(msgs[0].String(), conn.String()) {
			t.Errorf("expected the host %q in %q", conn.String(), msgs[0].String())
		}
	})

	t.Run("warns once however often Connect is retried", func(t *testing.T) {
		logger := &rigtest.MockLogger{}
		conn := connectExpectingFailure(t, insecureTLS, logger)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		for range 3 {
			_ = conn.Connect(ctx)
		}
		if logger.Len() != 1 {
			t.Fatalf("expected 1 log message after 4 Connect calls, got %d: %v", logger.Len(), logger.Messages())
		}
	})

	t.Run("the password is never logged", func(t *testing.T) {
		logger := &rigtest.MockLogger{}
		connectExpectingFailure(t, insecureTLS, logger)
		for _, m := range logger.Messages() {
			if strings.Contains(m.String(), "hunter2") {
				t.Fatalf("password leaked into the log: %q", m.String())
			}
		}
	})

	t.Run("plain http warns about cleartext instead", func(t *testing.T) {
		logger := &rigtest.MockLogger{}
		connectExpectingFailure(t, Config{
			Address:  "10.0.0.1",
			User:     "Administrator",
			Password: "hunter2",
		}, logger)
		if !logger.ReceivedSubstring("cleartext HTTP basic authentication") {
			t.Fatalf("expected the cleartext warning, got %v", logger.Messages())
		}
	})

	t.Run("port 5986 implies https, so the certificate case warns", func(t *testing.T) {
		logger := &rigtest.MockLogger{}
		// SetDefaults flips UseHTTPS for port 5986; without it this config
		// would read as plain HTTP and produce the cleartext warning.
		connectExpectingFailure(t, Config{
			Address:  "10.0.0.1",
			User:     "Administrator",
			Password: "hunter2",
			Port:     5986,
			Insecure: true,
		}, logger)
		if !logger.ReceivedSubstring("certificate verification disabled") {
			t.Fatalf("expected the certificate warning, got %v", logger.Messages())
		}
	})

	t.Run("silent when the transport protects the password", func(t *testing.T) {
		logger := &rigtest.MockLogger{}
		connectExpectingFailure(t, Config{
			Address:  "10.0.0.1",
			User:     "Administrator",
			Password: "hunter2",
			UseHTTPS: true,
		}, logger)
		if logger.Len() != 0 {
			t.Fatalf("expected no log messages, got %v", logger.Messages())
		}
	})
}
