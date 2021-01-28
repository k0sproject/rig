package darwin

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
	o := Darwin{}
	o.SetHost(h)
	require.Equal(t, "darwin", o.Kind())
}
