package powershell_test

import (
	"encoding/base64"
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/k0sproject/rig/v2/powershell"
	"github.com/stretchr/testify/require"
)

func TestCmdSimpleUsesEncoded(t *testing.T) {
	// Even a trivial one-liner is base64-encoded: the payload must be opaque to
	// an outer PowerShell login shell (OpenSSH DefaultShell=PowerShell) that
	// would otherwise expand its $-tokens before the inner powershell.exe runs.
	out := powershell.Cmd("$env:COMPUTERNAME")
	require.Contains(t, out, " -E ")
	require.NotContains(t, out, "-Command")
	require.Contains(t, decodeCmd(t, out), "$env:COMPUTERNAME")
}

func TestCmdNewlineUsesEncoded(t *testing.T) {
	out := powershell.Cmd("$a=1\n$b=2")
	require.Contains(t, out, " -E ")
	require.NotContains(t, out, "-Command")
}

func TestCmdDoubleQuoteUsesEncoded(t *testing.T) {
	out := powershell.Cmd(`New-Item -Path "C:\foo"`)
	require.Contains(t, out, " -E ")
	require.NotContains(t, out, "-Command")
}

func TestCmdSimpleInjectsProgressPreference(t *testing.T) {
	out := powershell.Cmd("$env:COMPUTERNAME")
	require.Contains(t, decodeCmd(t, out), "$ProgressPreference='SilentlyContinue'")
}

func TestCmdPercentUsesEncoded(t *testing.T) {
	// % is expanded by cmd.exe before PowerShell sees the command.
	out := powershell.Cmd("Write-Output %PATH%")
	require.Contains(t, out, " -E ")
	require.NotContains(t, out, "-Command")
}

func TestCmdExclamationUsesEncoded(t *testing.T) {
	// ! triggers delayed expansion in cmd.exe.
	out := powershell.Cmd("Write-Output !foo!")
	require.Contains(t, out, " -E ")
	require.NotContains(t, out, "-Command")
}

func TestCmdCmdExeMetacharsUseEncoded(t *testing.T) {
	// These cmd.exe metacharacters can alter semantics when the command is
	// executed via cmd.exe /c; -EncodedCommand keeps them opaque to the shell.
	metacharScripts := []string{
		`Write-Output ^escaped`,     // ^ escape character
		`Get-Process & Get-Service`, // & command chaining
		`Get-Process | Select Name`, // | pipe
		`Get-Content < file.txt`,    // < redirect
		`Get-Content > file.txt`,    // > redirect
	}
	for _, script := range metacharScripts {
		out := powershell.Cmd(script)
		require.Contains(t, out, " -E ", "expected -EncodedCommand for: %s", script)
		require.NotContains(t, out, "-Command", "unexpected -Command for: %s", script)
	}
}

func TestCmdBeginBlockSkipsProgressPrefix(t *testing.T) {
	script := "begin { } process { Get-Date }"
	out := powershell.Cmd(script)
	// Decode first: the output is base64, so a raw substring check would pass
	// trivially and prove nothing about the injected prefix.
	require.NotContains(t, decodeCmd(t, out), "$ProgressPreference")
}

// decodeCmd extracts the -EncodedCommand payload from a full Cmd() command
// line and decodes it back to the original PowerShell script.
func decodeCmd(t *testing.T, cmd string) string {
	t.Helper()
	const marker = " -E "
	idx := strings.Index(cmd, marker)
	require.NotEqual(t, -1, idx, "command must use -EncodedCommand: %q", cmd)
	return decodeEncodeCmd(t, cmd[idx+len(marker):])
}

// decodeEncodeCmd decodes a base64+UTF-16LE payload produced by EncodeCmd.
func decodeEncodeCmd(t *testing.T, encoded string) string {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, err)
	require.Equal(t, 0, len(raw)%2, "encoded payload must have even byte length (UTF-16LE)")
	words := make([]uint16, len(raw)/2)
	for i := range words {
		words[i] = uint16(raw[i*2]) | uint16(raw[i*2+1])<<8
	}
	runes := utf16.Decode(words)
	var sb strings.Builder
	for _, r := range runes {
		sb.WriteRune(r)
	}
	return sb.String()
}

func TestEncodeCmdBeginBlockSkipsProgressPrefix(t *testing.T) {
	script := "begin { } process { Get-Date }"
	decoded := decodeEncodeCmd(t, powershell.EncodeCmd(script))
	require.NotContains(t, decoded, "ProgressPreference")
}

func TestCmdBeginBlockNoSpaceSkipsProgressPrefix(t *testing.T) {
	// "begin{" without a space before the brace is also a valid begin block.
	script := "begin{ } process { Get-Date }"
	out := powershell.Cmd(script)
	require.NotContains(t, decodeCmd(t, out), "$ProgressPreference")
}

func TestCmdBeginBlockCaseInsensitiveSkipsProgressPrefix(t *testing.T) {
	// PowerShell keywords are case-insensitive; Begin/BEGIN must also be exempt.
	for _, script := range []string{
		"Begin { } Process { Get-Date }",
		"BEGIN { } PROCESS { Get-Date }",
	} {
		out := powershell.Cmd(script)
		require.NotContains(t, decodeCmd(t, out), "$ProgressPreference", "script: %s", script)
	}
}

func TestEncodeCmdUnicode(t *testing.T) {
	// Non-ASCII input must survive the UTF-16LE round-trip intact.
	script := "Write-Output 'héllo wörld 日本語'"
	require.False(t, utf8.ValidString(script) && len(script) == len([]rune(script)), "test must use multi-byte runes")
	decoded := decodeEncodeCmd(t, powershell.EncodeCmd(script))
	require.Contains(t, decoded, "héllo wörld 日本語")
}

func TestSingleQuote(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", `'hello'`},
		{"", `''`},
		{"it's", "'it`'s'"},
		{"back`tick", "'back``tick'"},
		{"line\nbreak", "'line`\nbreak'"},
		{"tab\there", "'tab`\there'"},
		{"\x00null", `'` + "`\x00null'"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := powershell.SingleQuote(tc.input)
			require.Equal(t, tc.want, got)
		})
	}
}

// TestSingleQuotePoolReuse calls SingleQuote repeatedly to exercise sync.Pool reuse
// and verify correctness is preserved across multiple uses of the same builder.
func TestSingleQuotePoolReuse(t *testing.T) {
	inputs := []string{"alpha", "beta's", "gamma`delta", "epsilon\nzeta"}
	for range 10 {
		for _, input := range inputs {
			got := powershell.SingleQuote(input)
			require.True(t, strings.HasPrefix(got, "'"), "expected opening quote: %s", got)
			require.True(t, strings.HasSuffix(got, "'"), "expected closing quote: %s", got)
		}
	}
}

func TestDoubleQuote(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", `"hello"`},
		{"", `""`},
		{`say "hi"`, "\"say `\"hi`\"\""},
		{`"already quoted"`, `"already quoted"`},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := powershell.DoubleQuote(tc.input)
			require.Equal(t, tc.want, got)
		})
	}
}

// TestDoubleQuotePoolReuse calls DoubleQuote repeatedly to exercise sync.Pool reuse.
func TestDoubleQuotePoolReuse(t *testing.T) {
	inputs := []string{"alpha", `beta "quoted"`, "gamma"}
	for range 10 {
		for _, input := range inputs {
			got := powershell.DoubleQuote(input)
			require.True(t, strings.HasPrefix(got, `"`), "expected opening double-quote: %s", got)
			require.True(t, strings.HasSuffix(got, `"`), "expected closing double-quote: %s", got)
		}
	}
}

func TestCompressedCmd(t *testing.T) {
	script := `
# This comment should be stripped
$a = 1
$b = 2
Write-Output ($a + $b)
`
	out := powershell.CompressedCmd(script)
	// CompressedCmd always encodes (the scriptlet contains newlines), so it
	// must use -EncodedCommand rather than -Command.
	require.Contains(t, out, "powershell.exe")
	require.Contains(t, out, " -E ")
}

// TestCompressedCmdPoolReuse calls CompressedCmd repeatedly to exercise the
// compressPool reuse path and verify the output is stable across calls.
func TestCompressedCmdPoolReuse(t *testing.T) {
	script := "$x = Get-Date\nWrite-Output $x"
	first := powershell.CompressedCmd(script)
	for range 10 {
		got := powershell.CompressedCmd(script)
		require.Equal(t, first, got, "CompressedCmd must be deterministic across pool reuse")
	}
}
