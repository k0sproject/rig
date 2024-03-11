//go:build windows

package homedir_test

import (
	"testing"

	"github.com/k0sproject/rig/v2/homedir"
	"github.com/stretchr/testify/assert"
)

func TestExpand(t *testing.T) {
	t.Setenv("USERPROFILE", "C:\\Users\\test")

	homeTmp, err := homedir.Expand("%USERPROFILE%\\tmp")
	assert.NoError(t, err)
	assert.Equal(t, "C:\\Users\\test\\tmp", homeTmp)

	tmp, err := homedir.Expand("C:\\tmp\\foo")
	assert.NoError(t, err)
	assert.Equal(t, "C:\\tmp\\foo", tmp)
}
