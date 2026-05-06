package kv_test

import (
	"fmt"
	"testing"

	"github.com/k0sproject/rig/v2/kv"
	"github.com/stretchr/testify/assert"
)

func TestSplitS(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		separator rune
		wantKey   string
		wantValue string
		wantErr   bool
	}{
		{
			name:      "Simple case",
			input:     "key=value",
			separator: '=',
			wantKey:   "key",
			wantValue: "value",
			wantErr:   false,
		},
		{
			name:      "Quoted key",
			input:     `"key with space"=value`,
			separator: '=',
			wantKey:   "key with space",
			wantValue: "value",
			wantErr:   false,
		},
		{
			name:      "Quoted value",
			input:     `key="value with space"`,
			separator: '=',
			wantKey:   "key",
			wantValue: "value with space",
			wantErr:   false,
		},
		{
			name:      "Both quoted",
			input:     `"key with space"="value with space"`,
			separator: '=',
			wantKey:   "key with space",
			wantValue: "value with space",
			wantErr:   false,
		},
		{
			name:      "Both quoted and escaped",
			input:     `"key \"with\" space"='value 'with' space'`,
			separator: '=',
			wantKey:   `key "with" space`,
			wantValue: `value with space`,
			wantErr:   false,
		},
		{
			name:      "Value ending with quotes",
			input:     `key="value with \"quotes\""`,
			separator: '=',
			wantKey:   "key",
			wantValue: `value with "quotes"`,
			wantErr:   false,
		},
		{
			name:      "Separator in quoted value",
			input:     `key="a=b"`,
			separator: '=',
			wantKey:   "key",
			wantValue: "a=b",
			wantErr:   false,
		},
		{
			name:      "Separator in single-quoted value",
			input:     `key='a=b'`,
			separator: '=',
			wantKey:   "key",
			wantValue: "a=b",
			wantErr:   false,
		},
		{
			name:      "Multiple separators",
			input:     "key=a=b=c",
			separator: '=',
			wantKey:   "key",
			wantValue: "a=b=c",
			wantErr:   false,
		},
		{
			name:      "No separator",
			input:     "justakey",
			separator: '=',
			wantErr:   true,
		},
		{
			name:      "Empty key with multiple separators",
			input:     "=a=b",
			separator: '=',
			wantKey:   "",
			wantValue: "a=b",
			wantErr:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key, value, err := kv.SplitRune(tc.input, tc.separator)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.wantKey, key)
				assert.Equal(t, tc.wantValue, value)
			}
		})
	}
}

func ExampleSplitRune() {
	key, value, _ := kv.SplitRune(`key="value in quotes"`, '=')
	fmt.Println("key:", key)
	fmt.Println("value:", value)
	// Output:
	// key: key
	// value: value in quotes
}
