package kv_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/k0sproject/rig/v2/kv"
)

// FuzzSplitRune verifies that SplitRune never panics on arbitrary input.
func FuzzSplitRune(f *testing.F) {
	// Seed corpus: typical key=value pairs, edge cases.
	f.Add("key=value")
	f.Add("KEY=\"quoted value\"")
	f.Add("k='single quoted'")
	f.Add("")
	f.Add("=")
	f.Add("noequals")
	f.Add(`k="unterminated`)
	f.Add("k=v=extra")
	f.Add(`"quoted_key"=val`)

	f.Fuzz(func(t *testing.T, input string) {
		if !utf8.ValidString(input) {
			return
		}
		// Must not panic; returned error is allowed.
		_, _, _ = kv.SplitRune(input, '=')
	})
}

// FuzzDecode verifies that Decoder.Decode never panics on arbitrary key=value text.
func FuzzDecode(f *testing.F) {
	f.Add("ID=ubuntu\nNAME=Ubuntu\nVERSION_ID=22.04\n")
	f.Add("key=value\n# comment\n")
	f.Add("")
	f.Add("=\n")
	f.Add("KEY=\"quoted value\"\n")

	f.Fuzz(func(t *testing.T, input string) {
		if !utf8.ValidString(input) {
			return
		}
		out := make(map[string]string)
		dec := kv.NewDecoder(strings.NewReader(input))
		// Must not panic; errors are acceptable.
		_ = dec.Decode(out)
	})
}
