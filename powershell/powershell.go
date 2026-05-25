// Package powershell provides helpers for powershell command generation
package powershell

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"strings"
	"sync"
	"unicode/utf16"
)

// PipeHasEnded string is used during the base64+sha265 upload process.
const PipeHasEnded = "The pipe has been ended."

// PipeIsBeingClosed string is used during the base64+sha265 upload process.
const PipeIsBeingClosed = "The pipe is being closed."

// compressBuf pairs a buffer with a gzip.Writer so both can be pooled together.
type compressBuf struct {
	buf *bytes.Buffer
	gz  *gzip.Writer
}

// compressPool recycles compressBuf instances to reduce allocations in CompressedCmd.
var compressPool = sync.Pool{
	New: func() any {
		buf := &bytes.Buffer{}
		gz, _ := gzip.NewWriterLevel(buf, gzip.BestCompression) // level is always valid
		return &compressBuf{buf: buf, gz: gz}
	},
}

// builderPool recycles strings.Builder instances for SingleQuote and DoubleQuote.
// Safety: Reset() nils the internal buffer, so a subsequent Grow allocates fresh
// memory; the string returned by String() before Reset keeps its own reference to
// the old backing array and remains valid after the builder is reused.
var builderPool = sync.Pool{
	New: func() any { return &strings.Builder{} },
}

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
	compBuf, ok := compressPool.Get().(*compressBuf)
	if !ok {
		buf := &bytes.Buffer{}
		gz, _ := gzip.NewWriterLevel(buf, gzip.BestCompression)
		compBuf = &compressBuf{buf: buf, gz: gz}
	}
	compBuf.buf.Reset()
	compBuf.gz.Reset(compBuf.buf)
	_, _ = compBuf.gz.Write([]byte(cmd))
	_ = compBuf.gz.Close()
	encoded := base64.StdEncoding.EncodeToString(compBuf.buf.Bytes())
	if compBuf.buf.Cap() <= 64<<10 {
		clear(compBuf.buf.Bytes()) // zero compressed bytes before pooling
		compBuf.buf.Reset()
		compressPool.Put(compBuf)
	}
	scriptlet := `$z="` + encoded + `"
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
func SingleQuote(str string) string {
	buf, ok := builderPool.Get().(*strings.Builder)
	if !ok {
		buf = &strings.Builder{}
	}
	defer func() {
		if buf.Cap() <= 64<<10 {
			buf.Reset()
			builderPool.Put(buf)
		}
	}()
	buf.Grow(len(str) + 3)
	buf.WriteRune('\'')
	for _, rune := range str {
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
func DoubleQuote(str string) string {
	if len(str) > 0 && str[0] == '"' && str[len(str)-1] == '"' {
		// already quoted
		return str
	}

	buf, ok := builderPool.Get().(*strings.Builder)
	if !ok {
		buf = &strings.Builder{}
	}
	defer func() {
		if buf.Cap() <= 64<<10 {
			buf.Reset()
			builderPool.Put(buf)
		}
	}()
	buf.Grow(len(str) + 4)
	buf.WriteRune('"')
	for _, rune := range str {
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

// SingleQuotePath single-quotes a path and converts forward slashes to backslashes.
// Use this instead of DoubleQuotePath when the value is passed to a -LiteralPath
// parameter or any context where PowerShell variable interpolation (e.g. '$' in
// directory names like C:\$Recycle.Bin) must not occur.
func SingleQuotePath(v string) string {
	return SingleQuote(ToWindowsPath(v))
}

// ToWindowsPath converts a unix-style forward slash separated path to a windows-style path.
func ToWindowsPath(v string) string {
	return strings.ReplaceAll(v, "/", "\\")
}
