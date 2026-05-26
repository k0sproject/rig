package powershell_test

import (
	"encoding/base64"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/k0sproject/rig/v2/powershell"
)

// FuzzCmd verifies that Cmd never panics and always produces a non-empty command string.
func FuzzCmd(f *testing.F) {
	f.Add("Get-Process")
	f.Add("Write-Host 'hello'")
	f.Add("Write-Host \"hello world\"")
	f.Add("")
	f.Add("cmd with\nnewline")
	f.Add("cmd % with ! special ^ chars & here | too < > end")

	f.Fuzz(func(t *testing.T, input string) {
		if !utf8.ValidString(input) {
			return
		}
		result := powershell.Cmd(input)
		if result == "" {
			t.Error("Cmd returned empty string")
		}
	})
}

// FuzzEncodeCmd verifies that EncodeCmd never panics and always produces valid base64.
func FuzzEncodeCmd(f *testing.F) {
	f.Add("Get-Process")
	f.Add("")
	f.Add("Write-Host 'hello world'")
	f.Add("$ProgressPreference='SilentlyContinue'")

	f.Fuzz(func(t *testing.T, input string) {
		if !utf8.ValidString(input) {
			return
		}
		result := powershell.EncodeCmd(input)
		if _, err := base64.StdEncoding.DecodeString(result); err != nil {
			t.Errorf("EncodeCmd(%q) returned invalid base64: %v", input, err)
		}
	})
}

// FuzzSingleQuote verifies that SingleQuote never panics and the result is always a
// single-quoted PS literal (starts and ends with ').
func FuzzSingleQuote(f *testing.F) {
	f.Add("hello")
	f.Add("")
	f.Add("it's a test")
	f.Add("line1\nline2")
	f.Add("\x00\x01\x02")

	f.Fuzz(func(t *testing.T, input string) {
		if !utf8.ValidString(input) {
			return
		}
		result := powershell.SingleQuote(input)
		if !strings.HasPrefix(result, "'") || !strings.HasSuffix(result, "'") {
			t.Errorf("SingleQuote(%q) = %q: want result wrapped in single quotes", input, result)
		}
	})
}

// FuzzDoubleQuote verifies that DoubleQuote never panics and the result is always
// double-quoted (starts and ends with ").
func FuzzDoubleQuote(f *testing.F) {
	f.Add("hello")
	f.Add("")
	f.Add(`say "hi"`)
	f.Add(`"already quoted"`)

	f.Fuzz(func(t *testing.T, input string) {
		if !utf8.ValidString(input) {
			return
		}
		result := powershell.DoubleQuote(input)
		if !strings.HasPrefix(result, "\"") || !strings.HasSuffix(result, "\"") {
			t.Errorf("DoubleQuote(%q) = %q: want result wrapped in double quotes", input, result)
		}
	})
}
