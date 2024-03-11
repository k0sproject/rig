package shellescape_test

import (
	"testing"

	"github.com/k0sproject/rig/v2/sh/shellescape"
	"github.com/stretchr/testify/assert"
)

func TestSplit(t *testing.T) {
	testCases := []struct {
		name       string
		input      string
		wantOutput []string
		wantErr    bool
	}{
		{
			name:       "Simple Double Quotes Single Value",
			input:      `"value"`,
			wantOutput: []string{"value"},
			wantErr:    false,
		},
		{
			name:       "Simple Unquoted Simple Value",
			input:      "value",
			wantOutput: []string{"value"},
			wantErr:    false,
		},
		{
			name:       "Two unquoted values",
			input:      "value1 value2",
			wantOutput: []string{"value1", "value2"},
			wantErr:    false,
		},
		{
			name:       "One unquoted and one single quoted value",
			input:      `value1 'value2 with quotes'`,
			wantOutput: []string{"value1", "value2 with quotes"},
			wantErr:    false,
		},
		{
			name:       "One unquoted and one double quoted value",
			input:      `value1 "value2 with quotes"`,
			wantOutput: []string{"value1", "value2 with quotes"},
			wantErr:    false,
		},
		{
			name:       "Double quoted value inside single quotes",
			input:      `value1 'value2 "with" quotes'`,
			wantOutput: []string{"value1", `value2 "with" quotes`},
			wantErr:    false,
		},
		{
			name:       "Escaped Single Quote in Single Quotes",
			input:      `value1 'escaped \'single\' quote'`,
			wantOutput: nil,
			// expecting an error due to unmatched quotes, first \' is treated as a literal and ' closes the
			// quotes. the second \ is treated as an escape for the ' and gets parsed as ' literal.
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotOutput, err := shellescape.Split(tc.input)
			if tc.wantErr {
				assert.Nil(t, gotOutput, "Split(%q)", tc.input)
				assert.Error(t, err, "Split(%q)", tc.input)
			} else {
				assert.NoError(t, err, "Split(%q)", tc.input)
				assert.Equal(t, tc.wantOutput, gotOutput, "Split(%q)", tc.input)
			}
		})
	}
}
