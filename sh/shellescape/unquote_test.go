package shellescape_test

import (
	"testing"
	"unicode/utf8"

	"github.com/k0sproject/rig/v2/sh/shellescape"
	"github.com/stretchr/testify/assert"
)

func TestUnquote(t *testing.T) {
	testCases := []struct {
		name       string
		input      string
		wantOutput string
		wantErr    bool
	}{
		{
			name:       "Simple Double Quotes",
			input:      `"value"`,
			wantOutput: "value",
			wantErr:    false,
		},
		{
			name:       "Simple Single Quotes",
			input:      `'value'`,
			wantOutput: "value",
			wantErr:    false,
		},
		{
			name:       "Escaped Double Quote",
			input:      `"value \"with\" quotes"`,
			wantOutput: `value "with" quotes`,
			wantErr:    false,
		},
		{
			name:       "Double quotes inside single quotes",
			input:      `'value "with" quotes'`,
			wantOutput: `value "with" quotes`,
			wantErr:    false,
		},
		{
			name:       "Single Quoted And Non-Quoted",
			input:      `'value 'with' quotes'`,
			wantOutput: `value with quotes`,
			wantErr:    false,
		},
		{
			name:       "Escaped Single Quote in Single Quotes",
			input:      `'escaped \'single\' quote'`,
			wantOutput: "",
			// expecting an error due to unmatched quotes, first \' is treated as a literal and ' closes the
			// quotes. the second \ is treated as an escape for the ' and gets parsed as ' literal.
			wantErr: true,
		},
		{
			name:       "Unmatched Quote",
			input:      `"value`,
			wantOutput: "",
			wantErr:    true,
		},
		{
			name:       "No Quotes",
			input:      "value",
			wantOutput: "value",
			wantErr:    false,
		},
		{
			name:       "Complex Quote Combinations",
			input:      `""''"'"'"'\'`,
			wantOutput: `'"'`,
			wantErr:    false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotOutput, err := shellescape.Unquote(tc.input)
			if tc.wantErr {
				assert.Error(t, err, "Unquote(%q)", tc.input)
			} else {
				assert.NoError(t, err, "Unquote(%q)", tc.input)
				assert.Equal(t, tc.wantOutput, gotOutput, "Unquote(%q)", tc.input)
			}
		})
	}
}

func FuzzQuoteUnquote(f *testing.F) {
	f.Fuzz(func(t *testing.T, orig string) {
		if !utf8.ValidString(orig) {
			return
		}

		// Step 1: Feed a string into shellescape.Quote
		quoted := shellescape.Quote(orig)

		// Step 2: Feed the output to shellescape.Unquote
		unquoted, err := shellescape.Unquote(quoted)
		assert.NoError(t, err)
		assert.Equal(t, orig, unquoted, "the quoted value was %q", quoted)
	})
}
