package enterpriselinux

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOSModuleComposition(t *testing.T) {
	o := CentOS{}
	require.Equal(t, "linux", o.Kind())
}
