package shellescape_test

import (
	"testing"

	"github.com/k0sproject/rig/shellescape"
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
