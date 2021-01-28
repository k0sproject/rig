package enterpriselinux

import (
	"testing"

	"github.com/k0sproject/rig"
	"github.com/stretchr/testify/require"
)

type host struct {
	rig.Connection
}

func TestOSModuleComposition(t *testing.T) {
	h := host{}
	o := CentOS{}
	o.SetHost(h)
	require.Equal(t, "linux", o.Kind())
}
