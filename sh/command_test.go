package sh_test

import (
	"testing"

	"github.com/k0sproject/rig/v2/sh"
	"github.com/stretchr/testify/assert"
)

func TestCommand(t *testing.T) {
	assert.Equal(t, "echo foo", sh.Command("echo", "foo"))
	assert.Equal(t, "echo foo bar", sh.Command("echo", "foo", "bar"))
	assert.Equal(t, "echo 'foo bar'", sh.Command("echo", "foo bar"))
}

func TestCommandBuilder(t *testing.T) {
	assert.Equal(t, "echo foo | grep -q foo", sh.CommandBuilder("echo").Arg("foo").Pipe("grep", "-q").Arg("foo").String())
	assert.Equal(t, "echo foo 'bar baz'", sh.CommandBuilder("echo").Args("foo", "bar baz").String())
}
