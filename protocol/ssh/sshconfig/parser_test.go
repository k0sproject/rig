package sshconfig_test

import (
	"testing"

	"github.com/k0sproject/rig/v2/protocol/ssh/sshconfig"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	type hostconfig struct {
		sshconfig.RequiredFields
		IdentityFile sshconfig.PathListValue
	}
	parser, err := sshconfig.NewParser(nil)
	require.NoError(t, err)
	obj := &hostconfig{}
	obj.SetUser("")
	obj.SetHost("example.com")
	err = parser.Parse(obj)
	require.NoError(t, err)
}

func TestParseFull(t *testing.T) {
	type hostconfig struct {
		sshconfig.SSHConfig
	}
	parser, err := sshconfig.NewParser(nil)
	require.NoError(t, err)
	obj := &hostconfig{}
	obj.SetUser("")
	obj.SetHost("example.com")
	err = parser.Parse(obj)
	require.NoError(t, err)
}
