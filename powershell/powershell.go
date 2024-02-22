// Package powershell provides helpers for powershell command generation
package powershell

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"strings"
)

// PipeHasEnded string is used during the base64+sha265 upload process.
const PipeHasEnded = "The pipe has been ended."

// PipeIsBeingClosed string is used during the base64+sha265 upload process.
const PipeIsBeingClosed = "The pipe is being closed."

// CompressedCmd creates a scriptlet that will decompress and execute a gzipped script to both avoid
// command line length limits and to reduce data transferred.
func CompressedCmd(psCmd string) string {
	var trimmed []string //nolint:prealloc
	lines := strings.Split(psCmd, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		trimmed = append(trimmed, line)
	}
	cmd := strings.Join(trimmed, "\n")
	var b bytes.Buffer
	w, _ := gzip.NewWriterLevel(&b, gzip.BestCompression)
	_, _ = w.Write([]byte(cmd))
	_ = w.Close()
	scriptlet := `$z="` + base64.StdEncoding.EncodeToString(b.Bytes()) + `"
$d=[Convert]::FromBase64String($z)
Set-Alias NO New-Object
$m=NO IO.MemoryStream
$m.Write($d,0,$d.Length)
$m.Seek(0,0)|Out-Null
$c=NO IO.Compression.GZipStream($m,[IO.Compression.CompressionMode]::Decompress)
$s=NO IO.StreamReader($c)
$u=$s.ReadToEnd()
$z=$null
Invoke-Expression "function s(){$u}"; s`
	return Cmd(scriptlet)
}

// EncodeCmd base64-encodes a string in a way that is accepted by PowerShell -EncodedCommand.
func EncodeCmd(psCmd string) string {
	if !strings.Contains(psCmd, "begin {") {
		psCmd = "$ProgressPreference='SilentlyContinue'; " + psCmd
	}
	// 2 byte chars to make PowerShell happy
	wideCmd := ""
	for _, b := range []byte(psCmd) {
		wideCmd += string(b) + "\x00"
	}

	// Base64 encode the command
	input := []uint8(wideCmd)
	return base64.StdEncoding.EncodeToString(input)
}

// Cmd builds a command-line for executing a complex command or script as an EncodedCommand through powershell.
func Cmd(psCmd string) string {
	encodedCmd := EncodeCmd(psCmd)

	// Create the powershell.exe command line to execute the script
	return "powershell.exe -NonInteractive -ExecutionPolicy Unrestricted -NoP -E " + encodedCmd
}

// SingleQuote quotes and escapes a string in a format that is accepted by powershell scriptlets
// from jbrekelmans/go-winrm/util.go PowerShellSingleQuotedStringLiteral.
func SingleQuote(v string) string {
	var buf strings.Builder
	buf.Grow(len(v) + 3)
	buf.WriteRune('\'')
	for _, rune := range v {
		switch rune {
		case '\n', '\r', '\t', '\v', '\f', '\a', '\b', '\'', '`', '\x00':
			buf.WriteString("`")
			buf.WriteRune(rune)
		default:
			buf.WriteRune(rune)
		}
	}
	buf.WriteRune('\'')
	return buf.String()
}

// DoubleQuote adds double quotes around a string and escapes any double quotes inside.
func DoubleQuote(v string) string {
	if v[0] == '"' && v[len(v)-1] == '"' {
		// already quoted
		return v
	}

	var buf strings.Builder
	buf.Grow(len(v) + 4)
	buf.WriteRune('"')
	for _, rune := range v {
		switch rune {
		case '"':
			buf.WriteString("`\"")
		default:
			buf.WriteRune(rune)
		}
	}
	buf.WriteRune('"')
	return buf.String()
}

// DoubleQuotePath adds double quotes around a string and escapes any double quotes inside.
// It also converts forward slashes to backslashes.
func DoubleQuotePath(v string) string {
	return DoubleQuote(ToWindowsPath(v))
}

// ToWindowsPath converts a unix-style forward slash separated path to a windows-style path.
func ToWindowsPath(v string) string {
	return strings.ReplaceAll(v, "/", "\\")
}
