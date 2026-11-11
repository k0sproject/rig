package winrm

import (
	"errors"
	"strings"
	"testing"

	"github.com/k0sproject/rig/v2/protocol"
)

// A password on a transport that does not protect it is refused unless the
// caller set insecure, and a client certificate is refused where it would be
// ignored.
func TestValidateRejectsExposedPassword(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  Config
		// noDefaults validates without SetDefaults, as a caller validating
		// a freshly unmarshaled config would.
		noDefaults bool
		reject     bool
	}{
		{
			name:   "password over plain HTTP",
			cfg:    Config{Address: "10.0.0.1", User: "Administrator", Password: "hunter2"},
			reject: true,
		},
		{
			name:   "NTLM does not protect the body",
			cfg:    Config{Address: "10.0.0.1", User: "Administrator", Password: "hunter2", UseNTLM: true},
			reject: true,
		},
		{
			name: "plain HTTP accepted once insecure is set",
			cfg:  Config{Address: "10.0.0.1", User: "Administrator", Password: "hunter2", Insecure: true},
		},
		{
			name: "HTTPS",
			cfg:  Config{Address: "10.0.0.1", User: "Administrator", Password: "hunter2", UseHTTPS: true},
		},
		{
			name: "HTTPS inferred from port 5986",
			cfg:  Config{Address: "10.0.0.1", Port: 5986, User: "Administrator", Password: "hunter2"},
		},
		{
			name:       "HTTPS inferred from port 5986 before SetDefaults",
			cfg:        Config{Address: "10.0.0.1", Port: 5986, User: "Administrator", Password: "hunter2"},
			noDefaults: true,
		},
		{
			name:       "certificate over port 5986 before SetDefaults",
			cfg:        Config{Address: "10.0.0.1", Port: 5986, CertPath: "/dev/null", KeyPath: "/dev/null"},
			noDefaults: true,
		},
		{
			name:       "password over port 5985 before SetDefaults",
			cfg:        Config{Address: "10.0.0.1", Port: 5985, User: "Administrator", Password: "hunter2"},
			noDefaults: true,
			reject:     true,
		},
		{
			name: "HTTPS without certificate verification",
			cfg:  Config{Address: "10.0.0.1", User: "Administrator", Password: "hunter2", UseHTTPS: true, Insecure: true},
		},
		{
			name: "certificate authentication over HTTPS",
			cfg:  Config{Address: "10.0.0.1", CertPath: "/dev/null", KeyPath: "/dev/null", UseHTTPS: true},
		},
		{
			name: "certificate and password over HTTPS",
			cfg:  Config{Address: "10.0.0.1", User: "Administrator", Password: "hunter2", CertPath: "/dev/null", KeyPath: "/dev/null", UseHTTPS: true},
		},
		{
			name:   "certificate over plain HTTP would be ignored",
			cfg:    Config{Address: "10.0.0.1", CertPath: "/dev/null", KeyPath: "/dev/null"},
			reject: true,
		},
		{
			name:   "certificate over plain HTTP with insecure would still be ignored",
			cfg:    Config{Address: "10.0.0.1", CertPath: "/dev/null", KeyPath: "/dev/null", Insecure: true},
			reject: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.cfg
			if !tc.noDefaults {
				cfg.SetDefaults()
			}
			err := cfg.Validate()

			if !tc.reject {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}

			if err == nil {
				t.Fatal("Validate() = nil, want a validation error")
			}
			if !errors.Is(err, protocol.ErrValidationFailed) {
				t.Errorf("Validate() = %v, want it to wrap protocol.ErrValidationFailed", err)
			}
			if cfg.Password != "" && strings.Contains(err.Error(), cfg.Password) {
				t.Error("the error repeats the password")
			}
		})
	}
}

// Validate is optional for callers, so the connection path must refuse the
// exposure on its own.
func TestConnectionRejectsExposedPassword(t *testing.T) {
	cfg := Config{Address: "10.0.0.1", User: "Administrator", Password: "hunter2"}

	conn, err := cfg.Connection()
	if err == nil {
		t.Fatal("Connection() error = nil, want a validation error")
	}
	if conn != nil {
		t.Errorf("Connection() returned a connection alongside error %v", err)
	}
	if !errors.Is(err, protocol.ErrValidationFailed) {
		t.Errorf("Connection() error = %v, want it to wrap protocol.ErrValidationFailed", err)
	}
	if !errors.Is(err, protocol.ErrNonRetryable) {
		t.Errorf("Connection() error = %v, want it to wrap protocol.ErrNonRetryable", err)
	}
	if strings.Contains(err.Error(), cfg.Password) {
		t.Error("the error repeats the password")
	}

	cfg.Insecure = true
	if _, err := cfg.Connection(); err != nil {
		t.Errorf("Connection() with insecure error = %v, want nil", err)
	}
}
