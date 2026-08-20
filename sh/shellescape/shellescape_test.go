package shellescape_test

import (
	"fmt"
	"testing"

	"github.com/k0sproject/rig/v2/sh/shellescape"
	"github.com/stretchr/testify/assert"
)

func TestQuote(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"Empty String", "", "''"},
		{"Double Quoted String", `"double quoted"`, `'"double quoted"'`},
		{"String with spaces", "with spaces", `'with spaces'`},
		{"Single Quoted String", `'single quoted'`, `''"'"'single quoted'"'"''`},
		{"Single Invalid", ";", `';'`}, // this could be returned as \;
		{"All Invalid", `;${}`, `';${}'`},
		{"Clean String", "foo.example.com", `foo.example.com`},
		// An unquoted backtick would have the shell run the command inside it,
		// so a filename like this must not come back verbatim.
		{"Command Substitution", "`id`", "'`id`'"},
		// Caret is outside the safe set of the implementation this package
		// replaces, and was a pipe in pre-POSIX shells.
		{"Caret", "^ID=", `'^ID='`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, shellescape.Quote(tt.input))
		})
	}
}

func TestQuoteForLoginShell(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty", input: "", want: "''"},
		{name: "nothing to quote", input: "foo", want: "foo"},
		{name: "spaces", input: "foo bar", want: "'foo bar'"},
		{name: "single quote", input: "foo'bar", want: `'foo'"'"'bar'`},
		{name: "backslash leaves the quoted run", input: `foo\bar`, want: `'foo'\\'bar'`},
		{name: "consecutive backslashes", input: `foo\\bar`, want: `'foo'\\''\\'bar'`},
		{name: "backslash before quote", input: `\'`, want: `''\\''"'"''`},
		{name: "other specials stay quoted", input: `$foo ${bar} *`, want: `'$foo ${bar} *'`},
		// An unquoted backtick would have the shell run the command inside it.
		{name: "backticks", input: "`id`", want: "'`id`'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, shellescape.QuoteForLoginShell(tt.input))
		})
	}
}

// TestQuotePreservesInvalidUTF8 guards against decoding the input into runes. A
// file name on a POSIX host is a byte string that need not be valid UTF-8, and
// rewriting an invalid byte into U+FFFD would quote a name other than the one
// asked for. The input here also carries a space and a quote, so both the
// wrapping and the escaping paths are taken.
func TestQuotePreservesInvalidUTF8(t *testing.T) {
	const name = "/tmp/a\xffb 'c"

	tests := []struct {
		name string
		got  string
	}{
		{"Quote", shellescape.Quote(name)},
		{"QuoteForLoginShell", shellescape.QuoteForLoginShell(name)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Contains(t, tt.got, "\xff", "the invalid byte must survive verbatim")
			assert.NotContains(t, tt.got, "�", "the invalid byte must not become the replacement character")
		})
	}
}

func TestJoin(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected string
	}{
		{"Basic", []string{"ls", "-l", "file with space"}, `ls -l 'file with space'`},
		{"Single quote in arg", []string{"touch", "it's here"}, `touch 'it'"'"'s here'`},
		{"Single quote only", []string{"echo", "'"}, `echo ''"'"''`},
		{"Single quote and special", []string{"rm", "/path/it's bad"}, `rm '/path/it'"'"'s bad'`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, shellescape.Join(tt.input...))
		})
	}
}

func TestQuoteCommand(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected string
	}{
		{"Basic Command", []string{"ls", "-l", "file with space"}, `ls -l 'file with space'`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, shellescape.QuoteCommand(tt.input))
		})
	}
}

func TestStripUnsafe(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"Printable", "Hello, World!", "Hello, World!"},
		{"Mixed", "\x00\x01\x02Test\x03\x04\x05", "Test"},
		{"SpecialChars", "SpecialChars\x1f\x7f", "SpecialChars"},
		{"Unicode", "中文测试", "中文测试"},
		{"Empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, shellescape.StripUnsafe(tt.in))
		})
	}
}

// This example demonstrates how to use shellescape.Quote to escape a string
// for use as an argument to a shell command.
func ExampleQuote() {
	quoted := shellescape.Quote("value with spaces")
	fmt.Println(quoted)
	// Output: 'value with spaces'
}

// This example demonstrates how to use shellescape.QuoteCommand to escape a
// command and its arguments for use in a shell command.
func ExampleQuoteCommand() {
	quoted := shellescape.QuoteCommand([]string{"ls", "-l", "file with space"})
	fmt.Println(quoted)
	// Output: ls -l 'file with space'
}
