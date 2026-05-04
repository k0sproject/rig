// Package powershell provides helpers for powershell command generation
package powershell

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"strings"
	"unicode/utf16"
)

// PipeHasEnded string is used during the base64+sha265 upload process.
const PipeHasEnded = "The pipe has been ended."

// PipeIsBeingClosed string is used during the base64+sha265 upload process.
const PipeIsBeingClosed = "The pipe is being closed."

// CompressedCmd creates a scriptlet that will decompress and execute a gzipped script to both avoid
// command line length limits and to reduce data transferred.
func CompressedCmd(psCmd string) string {
	var trimmed []string
	for line := range strings.SplitSeq(psCmd, "\n") {
		line = strings.TrimSpace(line)
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		trimmed = append(trimmed, line)
	}
	cmd := strings.Join(trimmed, "\n")
	var b bytes.Buffer
	w, err := gzip.NewWriterLevel(&b, gzip.BestCompression)
	if err != nil {
		panic(err) // BestCompression level is always valid
	}
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

// withProgressPreference prepends the $ProgressPreference suppressor unless the
// script already uses a begin block (handles it itself). The check is
// case-insensitive ("Begin {", "BEGIN{", etc.) to match PowerShell's own
// keyword handling.
func withProgressPreference(psCmd string) string {
	lower := strings.ToLower(psCmd)
	if strings.Contains(lower, "begin{") || strings.Contains(lower, "begin {") {
		return psCmd
	}
	return "$ProgressPreference='SilentlyContinue'; " + psCmd
}

// EncodeCmd base64-encodes a string as UTF-16LE in a way that is accepted by
// PowerShell -EncodedCommand.
func EncodeCmd(psCmd string) string {
	psCmd = withProgressPreference(psCmd)
	words := utf16.Encode([]rune(psCmd))
	buf := make([]byte, len(words)*2)
	for i, w := range words {
		buf[i*2] = byte(w) //nolint:gosec // G115: intentional low-8-bits extraction for little-endian encoding
		buf[i*2+1] = byte(w >> 8)
	}
	return base64.StdEncoding.EncodeToString(buf)
}

// Cmd builds a command-line for executing a PowerShell command or script.
// Scripts that contain newlines, double-quotes, or cmd.exe metacharacters
// are passed via -EncodedCommand to avoid shell expansion; simple one-liners
// are passed via -Command so they remain readable in logs.
// cmd.exe metacharacters guarded: " % ! ^ & | < >.
func Cmd(psCmd string) string {
	if strings.ContainsAny(psCmd, "\n\r\"%!^&|<>") {
		return "powershell.exe -NonInteractive -ExecutionPolicy Unrestricted -NoP -E " + EncodeCmd(psCmd)
	}
	return "powershell.exe -NonInteractive -ExecutionPolicy Unrestricted -NoP -Command \"" + withProgressPreference(psCmd) + "\""
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
	if len(v) > 0 && v[0] == '"' && v[len(v)-1] == '"' {
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
