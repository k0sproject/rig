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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, shellescape.Quote(tt.input))
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
