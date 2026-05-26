package powershell_test

import (
	"testing"

	"github.com/k0sproject/rig/v2/powershell"
	"github.com/stretchr/testify/assert"
)

// TestCmdGolden pins the exact command strings produced by Cmd for a set of
// realistic PowerShell fixtures. Any formatting change — whitespace, flag
// ordering, quoting — will cause a failure and requires a deliberate update.
func TestCmdGolden(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple one-liner uses -Command",
			input: "Get-Service k0s",
			want:  `powershell.exe -NonInteractive -ExecutionPolicy Unrestricted -NoP -Command "$ProgressPreference='SilentlyContinue'; Get-Service k0s"`,
		},
		{
			name:  "pipe metachar forces -EncodedCommand",
			input: "Get-Service k0s | Select Status",
			want:  `powershell.exe -NonInteractive -ExecutionPolicy Unrestricted -NoP -E JABQAHIAbwBnAHIAZQBzAHMAUAByAGUAZgBlAHIAZQBuAGMAZQA9ACcAUwBpAGwAZQBuAHQAbAB5AEMAbwBuAHQAaQBuAHUAZQAnADsAIABHAGUAdAAtAFMAZQByAHYAaQBjAGUAIABrADAAcwAgAHwAIABTAGUAbABlAGMAdAAgAFMAdABhAHQAdQBzAA==`,
		},
		{
			name:  "newline forces -EncodedCommand",
			input: "$a = 1\n$b = 2\nWrite-Output $a,$b",
			want:  `powershell.exe -NonInteractive -ExecutionPolicy Unrestricted -NoP -E JABQAHIAbwBnAHIAZQBzAHMAUAByAGUAZgBlAHIAZQBuAGMAZQA9ACcAUwBpAGwAZQBuAHQAbAB5AEMAbwBuAHQAaQBuAHUAZQAnADsAIAAkAGEAIAA9ACAAMQAKACQAYgAgAD0AIAAyAAoAVwByAGkAdABlAC0ATwB1AHQAcAB1AHQAIAAkAGEALAAkAGIA`,
		},
		{
			name:  "double quote forces -EncodedCommand",
			input: `New-Item -Path "C:\k0s"`,
			want:  `powershell.exe -NonInteractive -ExecutionPolicy Unrestricted -NoP -E JABQAHIAbwBnAHIAZQBzAHMAUAByAGUAZgBlAHIAZQBuAGMAZQA9ACcAUwBpAGwAZQBuAHQAbAB5AEMAbwBuAHQAaQBuAHUAZQAnADsAIABOAGUAdwAtAEkAdABlAG0AIAAtAFAAYQB0AGgAIAAiAEMAOgBcAGsAMABzACIA`,
		},
		{
			name:  "begin block skips ProgressPreference prefix",
			input: "begin { $x = 1 } process { Write-Output $x }",
			want:  `powershell.exe -NonInteractive -ExecutionPolicy Unrestricted -NoP -Command "begin { $x = 1 } process { Write-Output $x }"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := powershell.Cmd(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestSingleQuoteGolden pins exact SingleQuote output for realistic inputs.
func TestSingleQuoteGolden(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple string",
			input: "hello world",
			want:  `'hello world'`,
		},
		{
			name:  "embedded single quote",
			input: "it's here",
			want:  "'it`'s here'",
		},
		{
			name:  "windows path with backslashes",
			input: `C:\Users\Admin\Documents`,
			want:  `'C:\Users\Admin\Documents'`,
		},
		{
			name:  "string with embedded newline",
			input: "line1\nline2",
			want:  "'line1`\nline2'",
		},
		{
			name:  "empty string",
			input: "",
			want:  "''",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := powershell.SingleQuote(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestDoubleQuoteGolden pins exact DoubleQuote output for realistic inputs.
func TestDoubleQuoteGolden(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple string",
			input: "hello world",
			want:  `"hello world"`,
		},
		{
			name:  "embedded double quote is escaped with backtick",
			input: `say "hi"`,
			want:  "\"say `\"hi`\"\"",
		},
		{
			name:  "already-quoted string is returned unchanged",
			input: `"already quoted"`,
			want:  `"already quoted"`,
		},
		{
			name:  "empty string",
			input: "",
			want:  `""`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := powershell.DoubleQuote(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}
