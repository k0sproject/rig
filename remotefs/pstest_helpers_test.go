package remotefs_test

import (
	"encoding/base64"
	"strings"
	"unicode/utf16"
)

// decodePSScript decodes a powershell.exe -E <base64> command back to the
// original PowerShell source using proper UTF-16LE decoding. Returns ("", false)
// if the command is not in encoded form.
func decodePSScript(cmd string) (string, bool) {
	// Parse argv-style tokens and extract only the argument immediately
	// following -E or -EncodedCommand so benign flag reordering or trailing
	// arguments do not corrupt the base64 payload.
	fields := strings.Fields(cmd)
	var b64 string
	for i := 0; i < len(fields)-1; i++ {
		if fields[i] == "-E" || fields[i] == "-EncodedCommand" {
			b64 = fields[i+1]
			break
		}
	}
	if b64 == "" {
		return "", false
	}
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(data)%2 != 0 {
		return "", false
	}
	words := make([]uint16, len(data)/2)
	for i := range words {
		words[i] = uint16(data[i*2]) | uint16(data[i*2+1])<<8
	}
	return string(utf16.Decode(words)), true
}
