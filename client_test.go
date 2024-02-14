package rig_test

import (
	"testing"

	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/localhost"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
)

func TestConnectionWithConfigurer(t *testing.T) {
	cc := &rig.ClientConfig{
		Localhost: &localhost.Config{Enabled: true},
	}
	conn, err := rig.NewConnection(
		rig.WithClientConfigurer(cc),
	)
	require.NoError(t, err)
	require.NotNil(t, conn)

	require.NoError(t, conn.Connect())

	out, err := conn.ExecOutput("echo hello")
	require.NoError(t, err)
	require.Equal(t, "hello", out)
}

func TestConnectionWithClient(t *testing.T) {
	client, err := localhost.NewClient(localhost.Config{Enabled: true})
	require.NoError(t, err)
	conn, err := rig.NewConnection(rig.WithClient(client))
	require.NoError(t, err)
	require.NotNil(t, conn)

	require.NoError(t, conn.Connect())

	out, err := conn.ExecOutput("echo hello")
	require.NoError(t, err)
	require.Equal(t, "hello", out)
}

type testConfig struct {
	Hosts []*testHost `yaml:"hosts"`
}

type testHost struct {
	ClientConfig rig.ClientConfig `yaml:"-,inline"`
	*rig.Client
}

func (th *testHost) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type rawTestHost testHost
	h := (*rawTestHost)(th)
	if err := unmarshal(h); err != nil {
		return err
	}
	conn, err := rig.NewConnection(rig.WithClientConfigurer(&h.ClientConfig))
	if err != nil {
		return err
	}
	h.Client = conn
	return nil
}

func TestConnectionUnmarshal(t *testing.T) {
	hostConfig := map[string]any{
		"localhost": map[string]any{
			"enabled": true,
		},
	}
	mainConfig := map[string]any{
		"hosts": []map[string]any{hostConfig},
	}
	yamlContent, err := yaml.Marshal(mainConfig)
	require.NoError(t, err)

	testConfig := &testConfig{}
	require.NoError(t, yaml.Unmarshal(yamlContent, testConfig))
	require.Len(t, testConfig.Hosts, 1)
	conn := testConfig.Hosts[0]

	require.NoError(t, conn.Connect())

	require.Equal(t, "Local", conn.Protocol())

	require.NoError(t, conn.Connect())

	out, err := conn.ExecOutput("echo hello")
	require.NoError(t, err)
	require.Equal(t, "hello", out)
}

type testConfigConfigured struct {
	Hosts []*testHostConfigured `yaml:"hosts"`
}

type testHostConfigured struct {
	rig.DefaultClient `yaml:"-,inline"`
}

func TestConfiguredConnectionUnmarshal(t *testing.T) {
	hostConfig := map[string]any{
		"localhost": map[string]any{
			"enabled": true,
		},
	}
	mainConfig := map[string]any{
		"hosts": []map[string]any{hostConfig},
	}
	yamlContent, err := yaml.Marshal(mainConfig)
	require.NoError(t, err)

	testConfig := &testConfigConfigured{}
	require.NoError(t, yaml.Unmarshal(yamlContent, testConfig))
	require.Len(t, testConfig.Hosts, 1)
	conn := testConfig.Hosts[0]

	require.NoError(t, conn.Connect())

	require.Equal(t, "Local", conn.Protocol())

	require.NoError(t, conn.Connect())

	out, err := conn.ExecOutput("echo hello")
	require.NoError(t, err)
	require.Equal(t, "hello", out)
}
