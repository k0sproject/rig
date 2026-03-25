package rig_test

import (
	"testing"

	rig "github.com/k0sproject/rig/v2"
	"github.com/k0sproject/rig/v2/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
)

// k0sctlHost mirrors the k0sctl Host embedding pattern. Because ClientWithConfig
// has its own UnmarshalYAML, yaml.v2 passes the full document to it when it is
// encountered as an inline field — sibling fields on the outer struct (like Role)
// will NOT be populated unless the outer struct provides its own UnmarshalYAML.
// See TestK0sctlHostPattern for the correct approach.
type k0sctlHost struct {
	rig.ClientWithConfig `yaml:",inline"`
	Role                 string `yaml:"role"`
}

// k0sctlHostCorrect is how k0sctl should embed rig in v2: CompositeConfig inline
// for YAML, *Client separate and yaml:"-", own UnmarshalYAML to wire them together.
type k0sctlHostCorrect struct {
	rig.CompositeConfig `yaml:",inline"`
	*rig.Client         `yaml:"-"`
	Role                string `yaml:"role"`
}

func (h *k0sctlHostCorrect) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type hostAlias k0sctlHostCorrect
	alias := (*hostAlias)(h)
	if err := unmarshal(alias); err != nil {
		return err
	}
	client, err := rig.NewClient(rig.WithConnectionFactory(&h.CompositeConfig))
	if err != nil {
		return err
	}
	h.Client = client
	return nil
}

func unmarshalCompositeConfig(t *testing.T, src string) *rig.CompositeConfig {
	t.Helper()
	var cfg rig.CompositeConfig
	require.NoError(t, yaml.Unmarshal([]byte(src), &cfg))
	return &cfg
}

// --- SSH ---

func TestCompositeConfigSSH(t *testing.T) {
	cfg := unmarshalCompositeConfig(t, `
ssh:
  address: 10.0.0.1
  user: admin
  port: 2222
  keyPath: /home/user/.ssh/id_rsa
`)
	require.NotNil(t, cfg.SSH)
	assert.Equal(t, "10.0.0.1", cfg.SSH.Address)
	assert.Equal(t, "admin", cfg.SSH.User)
	assert.Equal(t, 2222, cfg.SSH.Port)
	require.NotNil(t, cfg.SSH.KeyPath)
	assert.Equal(t, "/home/user/.ssh/id_rsa", *cfg.SSH.KeyPath)
	assert.Nil(t, cfg.WinRM)
	assert.Nil(t, cfg.OpenSSH)
	assert.False(t, bool(cfg.Localhost))
}

func TestCompositeConfigSSHDefaults(t *testing.T) {
	cfg := unmarshalCompositeConfig(t, `
ssh:
  address: 192.168.1.1
`)
	require.NotNil(t, cfg.SSH)
	assert.Equal(t, "192.168.1.1", cfg.SSH.Address)
	// Port and User are not set by CompositeConfig itself; they come from
	// ssh.Config.SetDefaults (called at connection time). Verify zero values here
	// so we don't accidentally break default-setting in a later refactor.
	assert.Equal(t, 0, cfg.SSH.Port)
	assert.Equal(t, "", cfg.SSH.User)
}

func TestCompositeConfigSSHBastion(t *testing.T) {
	cfg := unmarshalCompositeConfig(t, `
ssh:
  address: 10.0.0.1
  user: root
  port: 22
  bastion:
    address: 1.2.3.4
    user: jump
    port: 22
`)
	require.NotNil(t, cfg.SSH)
	require.NotNil(t, cfg.SSH.Bastion)
	assert.Equal(t, "1.2.3.4", cfg.SSH.Bastion.Address)
	assert.Equal(t, "jump", cfg.SSH.Bastion.User)
}

// --- OpenSSH ---

func TestCompositeConfigOpenSSH(t *testing.T) {
	cfg := unmarshalCompositeConfig(t, `
openSSH:
  address: 10.0.0.2
  user: deploy
  port: 22
  keyPath: /home/deploy/.ssh/id_ed25519
  disableMultiplexing: true
`)
	require.NotNil(t, cfg.OpenSSH)
	assert.Equal(t, "10.0.0.2", cfg.OpenSSH.Address)
	require.NotNil(t, cfg.OpenSSH.User)
	assert.Equal(t, "deploy", *cfg.OpenSSH.User)
	require.NotNil(t, cfg.OpenSSH.Port)
	assert.Equal(t, 22, *cfg.OpenSSH.Port)
	require.NotNil(t, cfg.OpenSSH.KeyPath)
	assert.Equal(t, "/home/deploy/.ssh/id_ed25519", *cfg.OpenSSH.KeyPath)
	assert.True(t, cfg.OpenSSH.DisableMultiplexing)
	assert.Nil(t, cfg.SSH)
	assert.Nil(t, cfg.WinRM)
	assert.False(t, bool(cfg.Localhost))
}

func TestCompositeConfigOpenSSHMinimal(t *testing.T) {
	cfg := unmarshalCompositeConfig(t, `
openSSH:
  address: myhost.example.com
`)
	require.NotNil(t, cfg.OpenSSH)
	assert.Equal(t, "myhost.example.com", cfg.OpenSSH.Address)
	assert.Nil(t, cfg.OpenSSH.User)
	assert.Nil(t, cfg.OpenSSH.Port)
}

func TestCompositeConfigOpenSSHOptions(t *testing.T) {
	cfg := unmarshalCompositeConfig(t, `
openSSH:
  address: 10.0.0.3
  options:
    StrictHostKeyChecking: "no"
    ServerAliveInterval: 30
`)
	require.NotNil(t, cfg.OpenSSH)
	require.NotNil(t, cfg.OpenSSH.Options)
	assert.Equal(t, "no", cfg.OpenSSH.Options["StrictHostKeyChecking"])
}

// --- WinRM ---

func TestCompositeConfigWinRM(t *testing.T) {
	cfg := unmarshalCompositeConfig(t, `
winRM:
  address: 10.0.0.3
  user: Administrator
  port: 5985
  password: secret
  useHTTPS: false
  insecure: false
  useNTLM: false
`)
	require.NotNil(t, cfg.WinRM)
	assert.Equal(t, "10.0.0.3", cfg.WinRM.Address)
	assert.Equal(t, "Administrator", cfg.WinRM.User)
	assert.Equal(t, 5985, cfg.WinRM.Port)
	assert.Equal(t, "secret", cfg.WinRM.Password)
	assert.False(t, cfg.WinRM.UseHTTPS)
	assert.Nil(t, cfg.SSH)
	assert.Nil(t, cfg.OpenSSH)
	assert.False(t, bool(cfg.Localhost))
}

func TestCompositeConfigWinRMHTTPS(t *testing.T) {
	cfg := unmarshalCompositeConfig(t, `
winRM:
  address: 10.0.0.4
  user: Administrator
  port: 5986
  useHTTPS: true
`)
	require.NotNil(t, cfg.WinRM)
	assert.Equal(t, 5986, cfg.WinRM.Port)
	assert.True(t, cfg.WinRM.UseHTTPS)
}

func TestCompositeConfigWinRMWithBastion(t *testing.T) {
	cfg := unmarshalCompositeConfig(t, `
winRM:
  address: 10.0.0.5
  user: Administrator
  bastion:
    address: 1.2.3.4
    user: jump
    port: 22
`)
	require.NotNil(t, cfg.WinRM)
	require.NotNil(t, cfg.WinRM.Bastion)
	assert.Equal(t, "1.2.3.4", cfg.WinRM.Bastion.Address)
}

// --- Localhost ---

func TestCompositeConfigLocalhostBool(t *testing.T) {
	cfg := unmarshalCompositeConfig(t, `localhost: true`)
	assert.True(t, bool(cfg.Localhost))
	assert.Nil(t, cfg.SSH)
	assert.Nil(t, cfg.OpenSSH)
	assert.Nil(t, cfg.WinRM)
}

func TestCompositeConfigLocalhostFalse(t *testing.T) {
	cfg := unmarshalCompositeConfig(t, `localhost: false`)
	assert.False(t, bool(cfg.Localhost))
}

// TestCompositeConfigLocalhostV0Compat verifies that the v0.x struct form
// "localhost:\n  enabled: true" still parses correctly in v2, both when
// CompositeConfig is the direct unmarshal target and when it is inline.
func TestCompositeConfigLocalhostV0Compat(t *testing.T) {
	cfg := unmarshalCompositeConfig(t, `
localhost:
  enabled: true
`)
	assert.True(t, bool(cfg.Localhost))
}

func TestCompositeConfigLocalhostV0CompatDisabled(t *testing.T) {
	cfg := unmarshalCompositeConfig(t, `
localhost:
  enabled: false
`)
	assert.False(t, bool(cfg.Localhost))
}

// TestCompositeConfigLocalhostV0CompatInline verifies the v0.x form works when
// CompositeConfig is used inline (as it is inside ClientWithConfig). Because
// LocalhostConfig has its own UnmarshalYAML, this works without needing a
// CompositeConfig-level custom unmarshaler.
func TestCompositeConfigLocalhostV0CompatInline(t *testing.T) {
	h := &k0sctlHostCorrect{}
	require.NoError(t, yaml.Unmarshal([]byte(`
localhost:
  enabled: true
role: controller
`), h))
	assert.True(t, bool(h.CompositeConfig.Localhost))
	assert.Equal(t, "controller", h.Role)
}

// --- Validation ---

func TestCompositeConfigValidateNoProtocol(t *testing.T) {
	var cfg rig.CompositeConfig
	err := cfg.Validate()
	require.Error(t, err)
	assert.ErrorIs(t, err, protocol.ErrValidationFailed)
}

func TestCompositeConfigValidateMultipleProtocols(t *testing.T) {
	cfg := unmarshalCompositeConfig(t, `
ssh:
  address: 10.0.0.1
localhost: true
`)
	err := cfg.Validate()
	require.Error(t, err)
	assert.ErrorIs(t, err, protocol.ErrValidationFailed)
}

func TestCompositeConfigValidateSSHMissingAddress(t *testing.T) {
	cfg := unmarshalCompositeConfig(t, `
ssh:
  user: root
  port: 22
`)
	err := cfg.Validate()
	require.Error(t, err)
	assert.ErrorIs(t, err, protocol.ErrValidationFailed)
}

// --- Correct k0sctl embedding pattern ---

// TestK0sctlHostPattern verifies the recommended embedding pattern for k0sctl v2:
// embed CompositeConfig inline for YAML, *Client with yaml:"-", and provide a
// custom UnmarshalYAML on the outer struct. This correctly populates both the
// connection config and any sibling fields (e.g. Role).
func TestK0sctlHostPattern(t *testing.T) {
	var h k0sctlHostCorrect
	require.NoError(t, yaml.Unmarshal([]byte(`
ssh:
  address: 10.0.0.1
  user: root
  port: 22
role: controller
`), &h))
	require.NotNil(t, h.CompositeConfig.SSH)
	assert.Equal(t, "10.0.0.1", h.CompositeConfig.SSH.Address)
	assert.Equal(t, "controller", h.Role)
	assert.NotNil(t, h.Client)
}

func TestK0sctlHostPatternOpenSSH(t *testing.T) {
	var h k0sctlHostCorrect
	require.NoError(t, yaml.Unmarshal([]byte(`
openSSH:
  address: 10.0.0.2
role: worker
`), &h))
	require.NotNil(t, h.CompositeConfig.OpenSSH)
	assert.Equal(t, "10.0.0.2", h.CompositeConfig.OpenSSH.Address)
	assert.Equal(t, "worker", h.Role)
}

func TestK0sctlHostPatternLocalhost(t *testing.T) {
	var h k0sctlHostCorrect
	require.NoError(t, yaml.Unmarshal([]byte(`localhost: true`), &h))
	assert.True(t, bool(h.CompositeConfig.Localhost))
	assert.NotNil(t, h.Client)
}

func TestK0sctlHostPatternLocalhostV0Compat(t *testing.T) {
	var h k0sctlHostCorrect
	require.NoError(t, yaml.Unmarshal([]byte(`
localhost:
  enabled: true
`), &h))
	assert.True(t, bool(h.CompositeConfig.Localhost))
}

// --- ClientWithConfig inline limitation documentation ---

// TestClientWithConfigInlineSiblingFieldLimitation documents that when
// ClientWithConfig (which has its own UnmarshalYAML) is embedded inline,
// yaml.v2 passes the full document to ClientWithConfig.UnmarshalYAML, and
// sibling fields on the outer struct do NOT get populated. Use the
// k0sctlHostCorrect pattern instead.
func TestClientWithConfigInlineSiblingFieldLimitation(t *testing.T) {
	var h k0sctlHost
	require.NoError(t, yaml.Unmarshal([]byte(`
ssh:
  address: 10.0.0.1
  user: root
  port: 22
role: controller
`), &h))
	// Connection config IS populated (ClientWithConfig.UnmarshalYAML handles it).
	require.NotNil(t, h.ClientWithConfig.ConnectionConfig.SSH)
	// But Role is NOT populated — yaml.v2 limitation with custom UnmarshalYAML + inline.
	assert.Empty(t, h.Role, "sibling fields are not populated when ClientWithConfig is inline — use k0sctlHostCorrect pattern")
}

// --- Misc ---

// TestCompositeConfigUnknownFieldsIgnored verifies that extra fields present in
// v0.x configs (e.g. hostKey on ssh) are silently ignored rather than causing a
// parse error, preserving backward compatibility.
func TestCompositeConfigUnknownFieldsIgnored(t *testing.T) {
	cfg := unmarshalCompositeConfig(t, `
ssh:
  address: 10.0.0.1
  user: root
  port: 22
  hostKey: "ssh-rsa AAAA..."
`)
	require.NotNil(t, cfg.SSH)
	assert.Equal(t, "10.0.0.1", cfg.SSH.Address)
}

// TestCompositeConfigString verifies that String() doesn't panic on a
// valid config and returns a non-empty description.
func TestCompositeConfigString(t *testing.T) {
	cfg := unmarshalCompositeConfig(t, `
ssh:
  address: 10.0.0.1
  user: root
  port: 22
`)
	s := cfg.String()
	assert.NotEmpty(t, s)
	assert.Contains(t, s, "10.0.0.1")
}

func TestCompositeConfigStringNoProtocol(t *testing.T) {
	var cfg rig.CompositeConfig
	assert.NotPanics(t, func() { _ = cfg.String() })
}
