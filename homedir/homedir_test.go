//go:build !windows

package homedir_test

import (
	"testing"

	"github.com/k0sproject/rig/homedir"
	"github.com/stretchr/testify/assert"
)

func TestExpand(t *testing.T) {
	t.Setenv("HOME", "/home/test")

	homeTmp, err := homedir.Expand("~/tmp")
	assert.NoError(t, err)
	assert.Equal(t, "/home/test/tmp", homeTmp)

	tmp, err := homedir.Expand("/tmp/foo")
	assert.NoError(t, err)
	assert.Equal(t, "/tmp/foo", tmp)
}
