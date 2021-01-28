package darwin

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOSModuleComposition(t *testing.T) {
	o := Darwin{}
	require.Equal(t, "darwin", o.Kind())
}
