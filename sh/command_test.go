package sh_test

import (
	"fmt"
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

func ExampleCommand() {
	fmt.Println(sh.Command("echo", "foo bar"))
	// Output: echo 'foo bar'
}

func ExampleCommandBuilder_Pipe() {
	cmd := sh.CommandBuilder("echo").Arg("foo").Pipe("grep", "-q").Arg("foo")
	fmt.Println(cmd.String())
	// Output: echo foo | grep -q foo
}

func ExampleCommandBuilder_Arg() {
	cmd := sh.CommandBuilder("echo").Arg("foo").Arg("bar baz")
	fmt.Println(cmd.String())
	// Output: echo foo 'bar baz'
}

func ExampleCommandBuilder_ErrToNull() {
	cmd := sh.CommandBuilder("echo").Arg("foo").ErrToNull()
	fmt.Println(cmd.String())
	// Output: echo foo 2>/dev/null
}

func ExampleCommandBuilder_OutToNull() {
	cmd := sh.CommandBuilder("echo").Arg("foo").OutToNull()
	fmt.Println(cmd.String())
	// Output: echo foo >/dev/null
}

func ExampleCommandBuilder_ErrToOut() {
	cmd := sh.CommandBuilder("echo").Arg("foo").ErrToOut()
	fmt.Println(cmd.String())
	// Output: echo foo 2>&1
}

func ExampleCommandBuilder_OutToFile() {
	cmd := sh.CommandBuilder("echo").Arg("foo").OutToFile("file")
	fmt.Println(cmd.String())
	// Output: echo foo >file
}

func ExampleCommandBuilder_ErrToFile() {
	cmd := sh.CommandBuilder("echo").Arg("foo").ErrToFile("file")
	fmt.Println(cmd.String())
	// Output: echo foo 2>file
}

func ExampleCommandBuilder_AppendOutToFile() {
	cmd := sh.CommandBuilder("echo").Arg("foo").AppendOutToFile("file")
	fmt.Println(cmd.String())
	// Output: echo foo >>file
}

func ExampleCommandBuilder_AppendErrToFile() {
	cmd := sh.CommandBuilder("echo").Arg("foo").AppendErrToFile("file")
	fmt.Println(cmd.String())
	// Output: echo foo 2>>file
}

func ExampleCommandBuilder_Raw() {
	cmd := sh.CommandBuilder("ls").Raw("**/*.go")
	fmt.Println(cmd.String())
	// Output: ls **/*.go
}

func ExampleCommand_builder() {
	cmd := sh.CommandBuilder("echo").Arg("foo").ErrToNull().OutToFile("file").Pipe("grep").Args("-q", "foo bar")
	fmt.Println(cmd.String())
	// Output: echo foo 2>/dev/null >file | grep -q 'foo bar'
}

func ExampleQuote() {
	fmt.Println(sh.Quote("foo bar"))
	// Output: 'foo bar'
}
