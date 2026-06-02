package rig_test

import (
	"testing"

	rig "github.com/k0sproject/rig/v2"
	"github.com/k0sproject/rig/v2/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
)

// Tests in this file characterize how v2 handles v0.x YAML fixture shapes.
// They serve two purposes:
//   1. Accept: verify representative v0.x fixtures without removed fields parse strictly.
//   2. Reject: verify fields removed in v2 fail strict decoding instead of disappearing silently.

// --- SSH v0.x fixture ---

// TestSSHV0Fixture parses a representative v0.x SSH fixture containing the
// surviving SSH fields. It must decode strictly so migration-safe fields do not
// depend on permissive YAML loading.
func TestSSHV0Fixture(t *testing.T) {
	cfg := unmarshalCompositeConfigStrict(t, `
ssh:
  address: 10.0.0.1
  user: root
  port: 22
  keyPath: /home/root/.ssh/id_rsa
  bastion:
    address: 1.2.3.4
    user: jump
    port: 22
`)
	require.NotNil(t, cfg.SSH)
	assert.Equal(t, "10.0.0.1", cfg.SSH.Address)
	assert.Equal(t, "root", cfg.SSH.User)
	assert.Equal(t, 22, cfg.SSH.Port)
	require.NotNil(t, cfg.SSH.KeyPath)
	assert.Equal(t, "/home/root/.ssh/id_rsa", *cfg.SSH.KeyPath)
	require.NotNil(t, cfg.SSH.Bastion)
	assert.Equal(t, "1.2.3.4", cfg.SSH.Bastion.Address)
	assert.Equal(t, "jump", cfg.SSH.Bastion.User)
	assert.Equal(t, 22, cfg.SSH.Bastion.Port)
}

// TestSSHV0HostKeyRejected verifies that hostKey, a v0.x SSH field with no v2
// equivalent, fails strict decoding instead of being silently dropped.
func TestSSHV0HostKeyRejected(t *testing.T) {
	err := unmarshalCompositeConfigStrictError(`
ssh:
  address: 10.0.0.1
  user: root
  port: 22
  hostKey: "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQC..."
`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hostKey")
}

// TestSSHV0BastionHostKeyRejected verifies strict decoding also rejects removed
// v0.x fields nested in a bastion SSH config.
func TestSSHV0BastionHostKeyRejected(t *testing.T) {
	err := unmarshalCompositeConfigStrictError(`
ssh:
  address: 10.0.0.1
  user: root
  port: 22
  bastion:
    address: 1.2.3.4
    user: jump
    port: 22
    hostKey: "ssh-rsa AAAAB3NzaC1yc2E..."
`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hostKey")
}

// --- WinRM v0.x fixture ---

// TestWinRMV0CertAuthFixture verifies that a v0.x WinRM YAML with the full cert-auth
// field surface (caCertPath, certPath, keyPath, tlsServerName, useNTLM) parses cleanly
// in v2, confirming no field loss for the WinRM config.
func TestWinRMV0CertAuthFixture(t *testing.T) {
	cfg := unmarshalCompositeConfigStrict(t, `
winRM:
  address: 10.0.0.10
  user: Administrator
  port: 5986
  useHTTPS: true
  insecure: true
  useNTLM: true
  caCertPath: /certs/ca.pem
  certPath: /certs/client.pem
  keyPath: /certs/client-key.pem
  tlsServerName: winrm.internal.example.com
`)
	require.NotNil(t, cfg.WinRM)
	assert.Equal(t, "10.0.0.10", cfg.WinRM.Address)
	assert.Equal(t, "Administrator", cfg.WinRM.User)
	assert.Equal(t, 5986, cfg.WinRM.Port)
	assert.True(t, cfg.WinRM.UseHTTPS)
	assert.True(t, cfg.WinRM.Insecure)
	assert.True(t, cfg.WinRM.UseNTLM)
	assert.Equal(t, "/certs/ca.pem", cfg.WinRM.CACertPath)
	assert.Equal(t, "/certs/client.pem", cfg.WinRM.CertPath)
	assert.Equal(t, "/certs/client-key.pem", cfg.WinRM.KeyPath)
	assert.Equal(t, "winrm.internal.example.com", cfg.WinRM.TLSServerName)
}

// --- v0.x connection: wrapper ---

// TestV0ConnectionWrapperRejected verifies that the rig v0.x doc example showed
// embedding Connection as `yaml:"connection"`, which means YAML using a `connection:`
// nesting key must fail strict decoding in v2 instead of losing all protocol
// configuration.
func TestV0ConnectionWrapperRejected(t *testing.T) {
	err := unmarshalCompositeConfigStrictError(`
connection:
  ssh:
    address: 10.0.0.1
    user: root
    port: 22
`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection")
}

// TestV0ConnectionWrapperWinRMRejected verifies the same strict rejection for
// WinRM under a v0.x connection: wrapper.
func TestV0ConnectionWrapperWinRMRejected(t *testing.T) {
	err := unmarshalCompositeConfigStrictError(`
connection:
  winRM:
    address: 10.0.0.2
    user: Administrator
    port: 5985
`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection")
}

// --- Localhost reject paths ---

// TestLocalhostV0MapUnknownKey verifies that a localhost mapping with an unrecognised
// key is rejected — the only accepted key in the struct form is "enabled".
func TestLocalhostV0MapUnknownKey(t *testing.T) {
	var cfg rig.CompositeConfig
	err := yaml.Unmarshal([]byte("localhost:\n  foo: bar"), &cfg)
	require.Error(t, err)
	assert.ErrorIs(t, err, protocol.ErrValidationFailed)
}

// TestLocalhostV0MapNonBoolEnabled verifies that a localhost mapping where 'enabled'
// is not a boolean is rejected with ErrValidationFailed.
func TestLocalhostV0MapNonBoolEnabled(t *testing.T) {
	var cfg rig.CompositeConfig
	err := yaml.Unmarshal([]byte("localhost:\n  enabled: 42"), &cfg)
	require.Error(t, err)
	assert.ErrorIs(t, err, protocol.ErrValidationFailed)
}

func unmarshalCompositeConfigStrict(t *testing.T, src string) *rig.CompositeConfig {
	t.Helper()
	var cfg rig.CompositeConfig
	require.NoError(t, yaml.UnmarshalStrict([]byte(src), &cfg))
	return &cfg
}

func unmarshalCompositeConfigStrictError(src string) error {
	var cfg rig.CompositeConfig
	return yaml.UnmarshalStrict([]byte(src), &cfg)
}
