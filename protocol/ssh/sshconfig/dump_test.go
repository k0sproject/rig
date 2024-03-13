package sshconfig_test

import (
	"strings"
	"testing"

	"github.com/k0sproject/rig/v2/protocol/ssh/sshconfig"
	"github.com/k0sproject/rig/v2/rigtest"
	"github.com/stretchr/testify/require"
)

func TestDump(t *testing.T) {
	obj := &sshconfig.SSHConfig{}
	obj.SetHost("test")
	parser, err := sshconfig.NewParser(nil)
	require.NoError(t, err)
	rigtest.TraceToStderr()
	require.NoError(t, parser.Parse(obj))
	rigtest.TraceOff()
	content, err := sshconfig.Dump(obj)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(content, "Host test"), "content should start with 'Host test'")
	obj2 := &sshconfig.SSHConfig{}
	obj2.SetHost("test")
	parser, err = sshconfig.NewParser(strings.NewReader(content))
	require.NoError(t, err)
	require.NoError(t, parser.Parse(obj2))
	content2, err := sshconfig.Dump(obj2)
	require.NoError(t, err)
	require.Equal(t, content, content2)
	println(content)
}
