package winrm

import (
	"errors"
	"strings"
	"testing"

	"github.com/k0sproject/rig/v2/protocol"
)

// A password on a transport that does not protect it is refused unless the
// caller set insecure. Certificate authentication is only selected over
// HTTPS, so it is never reached by the rule.
func TestValidateRejectsExposedPassword(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cfg    Config
		reject bool
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
			name: "HTTPS without certificate verification",
			cfg:  Config{Address: "10.0.0.1", User: "Administrator", Password: "hunter2", UseHTTPS: true, Insecure: true},
		},
		{
			name: "certificate authentication carries no password",
			cfg:  Config{Address: "10.0.0.1", CertPath: "/dev/null", KeyPath: "/dev/null"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.cfg
			cfg.SetDefaults()
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
			if strings.Contains(err.Error(), cfg.Password) {
				t.Error("the error repeats the password")
			}
		})
	}
}
