package rig_test

import (
	"context"
	"testing"

	"github.com/k0sproject/rig/v2"
	"github.com/k0sproject/rig/v2/rigtest"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
)

func TestClientWithConfigurer(t *testing.T) {
	cc := &rig.CompositeConfig{
		Localhost: true,
	}
	conn, err := rig.NewClient(
		rig.WithConnectionConfigurer(cc),
	)
	require.NoError(t, err)
	require.NotNil(t, conn)

	require.NoError(t, conn.Connect(context.Background()))

	out, err := conn.ExecOutput("echo hello")
	require.NoError(t, err)
	require.Equal(t, "hello", out)
}

func TestClient(t *testing.T) {
	conn := rigtest.NewMockConnection()
	conn.AddCommandOutput(rigtest.Match("echo hello"), "hello")

	client, err := rig.NewClient(rig.WithConnection(conn))
	require.NoError(t, err)

	require.NoError(t, client.Connect(context.Background()))

	out, err := client.ExecOutput("echo hello")
	require.NoError(t, err)
	require.Equal(t, "hello", out)
}

func TestClientLogging(t *testing.T) {
	conn := rigtest.NewMockConnection()
	conn.AddCommandOutput(rigtest.Match("echo hello"), "hello")

	logger := &rigtest.MockLogger{}
	client, err := rig.NewClient(rig.WithConnection(conn), rig.WithLogger(logger))
	require.NoError(t, err)

	require.NoError(t, client.Connect(context.Background()))

	_, _ = client.ExecOutput("echo hello")

	t.Log(logger.Messages())
}

type testConfig struct {
	Hosts []*testHost `yaml:"hosts"`
}

type testHost struct {
	ClientConfig rig.CompositeConfig `yaml:"-,inline"`
	*rig.Client
}

func (th *testHost) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type rawTestHost testHost
	h := (*rawTestHost)(th)
	if err := unmarshal(h); err != nil {
		return err
	}
	conn, err := rig.NewClient(rig.WithConnectionConfigurer(&h.ClientConfig))
	if err != nil {
		return err
	}
	h.Client = conn
	return nil
}

func TestConnectionUnmarshal(t *testing.T) {
	hostConfig := map[string]any{
		"localhost": true,
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

	require.NoError(t, conn.Connect(context.Background()))

	require.Equal(t, "Local", conn.Protocol())

	require.NoError(t, conn.Connect(context.Background()))

	out, err := conn.ExecOutput("echo hello")
	require.NoError(t, err)
	require.Equal(t, "hello", out)
}

type testConfigConfigured struct {
	Hosts []*testHostConfigured `yaml:"hosts"`
}

type testHostConfigured struct {
	rig.ClientWithConfig `yaml:"-,inline"`
}

func TestConfiguredConnectionUnmarshal(t *testing.T) {
	hostConfig := map[string]any{
		"localhost": true,
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

	require.NoError(t, conn.Connect(context.Background()))

	require.Equal(t, "Local", conn.Protocol())

	require.NoError(t, conn.Connect(context.Background()))

	out, err := conn.ExecOutput("echo hello")
	require.NoError(t, err)
	require.Equal(t, "hello", out)
}
