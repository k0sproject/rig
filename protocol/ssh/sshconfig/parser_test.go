package sshconfig_test

import (
	"strings"
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

func TestParseIgnoreUnknown(t *testing.T) {
	t.Run("no ignoreunknown, no ErrorOnUnknown", func(t *testing.T) {
		type hostconfig struct {
			sshconfig.RequiredFields
			IdentityFile sshconfig.PathListValue
		}
		obj := &hostconfig{}
		obj.SetUser("")
		obj.SetHost("example.com")

		parser, err := sshconfig.NewParser(strings.NewReader("Host example.com\n  IdentityFile ~/.ssh/id_rsa\n  UnknownOption yes\n"))
		require.NoError(t, err)
		require.NoError(t, parser.Parse(obj))
	})
	t.Run("no ignoreunknown, ErrorOnUnknown true", func(t *testing.T) {
		type hostconfig struct {
			sshconfig.RequiredFields
			IdentityFile sshconfig.PathListValue
		}
		obj := &hostconfig{}
		obj.SetUser("")
		obj.SetHost("example.com")

		parser, err := sshconfig.NewParser(strings.NewReader("Host example.com\n  IdentityFile ~/.ssh/id_rsa\n  UnknownOption yes\n"))
		parser.ErrorOnUnknown = true
		require.NoError(t, err)
		err = parser.Parse(obj)
		require.Error(t, err)
		require.Contains(t, err.Error(), "unknown field")
	})
	t.Run("ignoreunknown set, ErrorOnUnknown true", func(t *testing.T) {
		obj := &sshconfig.SSHConfig{}
		obj.SetUser("")
		obj.SetHost("example.com")
		obj.IgnoreUnknown.SetString("unknown*", sshconfig.ValueOriginOption, "")
		parser, err := sshconfig.NewParser(strings.NewReader("Host example.com\n  IdentityFile ~/.ssh/id_rsa\n  UnknownOption yes\n"))
		parser.ErrorOnUnknown = true
		require.NoError(t, err)
		require.NoError(t, parser.Parse(obj))
	})
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
