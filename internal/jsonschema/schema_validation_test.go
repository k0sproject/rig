package main_test

import (
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func newCompiler() *jsonschema.Compiler {
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	return c
}

func compileSchema(t *testing.T, path string) *jsonschema.Schema {
	t.Helper()
	sch, err := newCompiler().Compile(path)
	if err != nil {
		t.Fatalf("compile %s: %v", path, err)
	}
	return sch
}

func validateJSON(sch *jsonschema.Schema, raw string) error {
	v, err := jsonschema.UnmarshalJSON(strings.NewReader(raw))
	if err != nil {
		return err
	}
	return sch.Validate(v)
}

func TestSchemaSSH(t *testing.T) {
	sch := compileSchema(t, "../../schemas/ssh.json")

	valid := []struct {
		name string
		doc  string
	}{
		{"ipv4 address", `{"address":"192.168.1.1"}`},
		{"ipv6 address", `{"address":"::1"}`},
		{"hostname", `{"address":"example.com"}`},
		{"hostname with port and user", `{"address":"host.example.com","user":"admin","port":2222}`},
		{"with key path", `{"address":"192.168.1.1","keyPath":"/home/user/.ssh/id_rsa"}`},
		{"with bastion", `{"address":"192.168.1.1","bastion":{"address":"10.0.0.1","user":"jump"}}`},
	}
	for _, tc := range valid {
		t.Run("valid/"+tc.name, func(t *testing.T) {
			if err := validateJSON(sch, tc.doc); err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
		})
	}

	invalid := []struct {
		name string
		doc  string
	}{
		{"missing address", `{"user":"root"}`},
		{"invalid address format", `{"address":"not a valid address!!"}`},
		{"port below minimum", `{"address":"192.168.1.1","port":0}`},
		{"port above maximum", `{"address":"192.168.1.1","port":65536}`},
		{"empty user", `{"address":"192.168.1.1","user":""}`},
		{"extra field", `{"address":"192.168.1.1","unknown":"value"}`},
	}
	for _, tc := range invalid {
		t.Run("invalid/"+tc.name, func(t *testing.T) {
			if err := validateJSON(sch, tc.doc); err == nil {
				t.Errorf("expected validation error for: %s", tc.doc)
			}
		})
	}
}

func TestSchemaOpenSSH(t *testing.T) {
	sch := compileSchema(t, "../../schemas/openssh.json")

	valid := []struct {
		name string
		doc  string
	}{
		{"ipv4 address", `{"address":"10.0.0.1"}`},
		{"hostname", `{"address":"example.com"}`},
		// OpenSSH accepts ssh_config aliases that look nothing like a hostname or IP.
		{"ssh config alias", `{"address":"prod-bastion"}`},
		{"with user and port", `{"address":"host.example.com","user":"deploy","port":22}`},
		{"with options", `{"address":"host.example.com","options":{"StrictHostKeyChecking":"no"}}`},
	}
	for _, tc := range valid {
		t.Run("valid/"+tc.name, func(t *testing.T) {
			if err := validateJSON(sch, tc.doc); err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
		})
	}

	invalid := []struct {
		name string
		doc  string
	}{
		{"missing address", `{"user":"admin"}`},
		{"port below minimum", `{"address":"host.example.com","port":0}`},
		{"port above maximum", `{"address":"host.example.com","port":70000}`},
		{"extra field", `{"address":"host.example.com","unknown":"value"}`},
	}
	for _, tc := range invalid {
		t.Run("invalid/"+tc.name, func(t *testing.T) {
			if err := validateJSON(sch, tc.doc); err == nil {
				t.Errorf("expected validation error for: %s", tc.doc)
			}
		})
	}
}

func TestSchemaWinRM(t *testing.T) {
	sch := compileSchema(t, "../../schemas/winrm.json")

	valid := []struct {
		name string
		doc  string
	}{
		{"ipv4 address", `{"address":"192.168.1.100"}`},
		{"hostname", `{"address":"winserver.example.com"}`},
		{"ipv6 address", `{"address":"2001:db8::1"}`},
		{"full config", `{"address":"192.168.1.100","user":"Administrator","port":5986,"useHTTPS":true}`},
		{"with tls server name as ip", `{"address":"192.168.1.100","tlsServerName":"192.168.1.100"}`},
		{"with tls server name as hostname", `{"address":"192.168.1.100","tlsServerName":"internal.example.com"}`},
	}
	for _, tc := range valid {
		t.Run("valid/"+tc.name, func(t *testing.T) {
			if err := validateJSON(sch, tc.doc); err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
		})
	}

	invalid := []struct {
		name string
		doc  string
	}{
		{"missing address", `{"user":"Administrator"}`},
		{"invalid address format", `{"address":"not-valid!!"}`},
		{"port below minimum", `{"address":"192.168.1.1","port":0}`},
		{"port above maximum", `{"address":"192.168.1.1","port":99999}`},
		{"invalid tls server name", `{"address":"192.168.1.1","tlsServerName":"not valid!!"}`},
		{"extra field", `{"address":"192.168.1.1","unknown":"value"}`},
	}
	for _, tc := range invalid {
		t.Run("invalid/"+tc.name, func(t *testing.T) {
			if err := validateJSON(sch, tc.doc); err == nil {
				t.Errorf("expected validation error for: %s", tc.doc)
			}
		})
	}
}

func TestSchemaLocalhost(t *testing.T) {
	sch := compileSchema(t, "../../schemas/localhost.json")

	valid := []struct {
		name string
		doc  string
	}{
		{"enabled true", `{"enabled":true}`},
	}
	for _, tc := range valid {
		t.Run("valid/"+tc.name, func(t *testing.T) {
			if err := validateJSON(sch, tc.doc); err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
		})
	}

	invalid := []struct {
		name string
		doc  string
	}{
		{"missing enabled", `{}`},
		{"enabled false", `{"enabled":false}`},
		{"extra field", `{"enabled":true,"host":"localhost"}`},
	}
	for _, tc := range invalid {
		t.Run("invalid/"+tc.name, func(t *testing.T) {
			if err := validateJSON(sch, tc.doc); err == nil {
				t.Errorf("expected validation error for: %s", tc.doc)
			}
		})
	}
}
