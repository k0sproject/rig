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

func TestCmdSimpleUsesCommand(t *testing.T) {
	out := powershell.Cmd("$env:COMPUTERNAME")
	require.Contains(t, out, "-Command")
	require.NotContains(t, out, " -E ")
	require.Contains(t, out, "$env:COMPUTERNAME")
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
	require.Contains(t, out, "$ProgressPreference='SilentlyContinue'")
}

func TestCmdSimpleReadableInLogs(t *testing.T) {
	script := "[DateTimeOffset]::UtcNow.ToUnixTimeSeconds()"
	out := powershell.Cmd(script)
	require.True(t, strings.Contains(out, script), "simple script should be visible in the command string")
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
	// executed via cmd.exe /c and must go through -EncodedCommand.
	// Note: () are NOT included — they are protected inside the double-quoted
	// -Command "..." argument and are ubiquitous in PowerShell method calls.
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
	require.NotContains(t, out, "$ProgressPreference")
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
	require.NotContains(t, out, "$ProgressPreference")
}

func TestCmdBeginBlockCaseInsensitiveSkipsProgressPrefix(t *testing.T) {
	// PowerShell keywords are case-insensitive; Begin/BEGIN must also be exempt.
	for _, script := range []string{
		"Begin { } Process { Get-Date }",
		"BEGIN { } PROCESS { Get-Date }",
	} {
		out := powershell.Cmd(script)
		require.NotContains(t, out, "$ProgressPreference", "script: %s", script)
	}
}

func TestEncodeCmdUnicode(t *testing.T) {
	// Non-ASCII input must survive the UTF-16LE round-trip intact.
	script := "Write-Output 'héllo wörld 日本語'"
	require.False(t, utf8.ValidString(script) && len(script) == len([]rune(script)), "test must use multi-byte runes")
	decoded := decodeEncodeCmd(t, powershell.EncodeCmd(script))
	require.Contains(t, decoded, "héllo wörld 日本語")
}
