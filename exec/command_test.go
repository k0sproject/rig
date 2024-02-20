package exec_test

import (
	"testing"

	"github.com/k0sproject/rig/exec"
	"github.com/stretchr/testify/assert"
)

func TestQuote(t *testing.T) {
	assert.Equal(t, "foo", exec.Quote("foo"))
	assert.Equal(t, "'foo bar'", exec.Quote("foo bar"))
}

func TestCommand(t *testing.T) {
	assert.Equal(t, "echo foo", exec.Command("echo", "foo"))
	assert.Equal(t, "echo foo bar", exec.Command("echo", "foo", "bar"))
	assert.Equal(t, "echo 'foo bar'", exec.Command("echo", "foo bar"))
}
