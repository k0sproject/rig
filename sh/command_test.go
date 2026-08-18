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

func TestShell(t *testing.T) {
	assert.Equal(t, `/bin/sh -c -- 'echo foo | tee bar'`, sh.Shell("echo foo | tee bar"))
	assert.Equal(t, `/bin/sh -c -- 'echo foo'`, sh.ShellWith("", "echo foo"))
	assert.Equal(t, `/usr/xpg4/bin/sh -c -- 'echo foo'`, sh.ShellWith("/usr/xpg4/bin/sh", "echo foo"))
}

// TestShellQuoting pins the quoting of payloads that a non-POSIX login shell
// would otherwise reinterpret on the way to the shell that runs them.
//
// The expectations were verified by hand by running each wrapped command through
// sh, bash, dash, zsh, ksh and fish. The integration suite exercises the wrapping
// end to end on every command it runs, both against the POSIX login shells of its
// images (bash, dash, ash) and against fish, which the
// rig_test_regular_user_fish_login_shell case in test/test.sh installs and assigns
// as the SSH user's login shell. What these fixtures add is the byte-exact quoting,
// which the integration suite can only observe indirectly.
func TestShellQuoting(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    string
	}{
		{name: "plain", command: "true", want: `/bin/sh -c -- true`},
		{name: "spaces", command: "echo foo bar", want: `/bin/sh -c -- 'echo foo bar'`},
		{name: "single quotes", command: `bash -c 'true'`, want: `/bin/sh -c -- 'bash -c '"'"'true'"'"''`},
		{name: "backslash", command: `printf a\b`, want: `/bin/sh -c -- 'printf a'\\'b'`},
		{name: "consecutive backslashes", command: `printf \\n`, want: `/bin/sh -c -- 'printf '\\''\\'n'`},
		{
			// The sed expression from os/darwin.go carries consecutive backslashes.
			name:    "darwin os release sed expression",
			command: `sed -E "s/^.*FOR (.+)\\\/\1/"`,
			want:    `/bin/sh -c -- 'sed -E "s/^.*FOR (.+)'\\''\\''\\'/'\\'1/"'`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, sh.Shell(tt.command))
		})
	}
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
