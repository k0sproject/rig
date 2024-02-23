package homedir_test

import (
	"testing"

	"github.com/k0sproject/rig/homedir"
	"github.com/stretchr/testify/assert"
)

func TestExpand(t *testing.T) {
	t.Setenv("HOME", "/home/test")

	home, err := homedir.Expand("~/tmp")
	assert.NoError(t, err)
	assert.Equal(t, home, "/home/test/tmp")

	home, err = homedir.Expand("/tmp")
	assert.NoError(t, err)
	assert.Equal(t, home, "/tmp")
}
