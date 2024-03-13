package sshconfig_test

import (
	"strings"
	"testing"

	"github.com/k0sproject/rig/v2/sshconfig"
	"github.com/stretchr/testify/require"
)

func TestPrinter(t *testing.T) {
	config, err := sshconfig.ConfigFor("example.com")
	require.NoError(t, err)
	configStr, err := sshconfig.Dump(config)
	require.NoError(t, err)
	parser, err := sshconfig.NewParser(strings.NewReader(configStr))
	require.NoError(t, err)
	configNew := &sshconfig.Config{}
	require.NoError(t, parser.Apply(configNew, "example.com"))
	configStr2, err := sshconfig.Dump(configNew)
	require.Equal(t, configStr, configStr2)
	require.Equal(t, config, configNew)
}
