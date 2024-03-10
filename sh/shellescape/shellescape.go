// Package shellescape provides functions to escape strings for use in posix shell commands.
//
// It is a drop-in replacement for gopkg.in/alessio/shellescape.v1.
//
// Additionally an Unquote function is provided.
package shellescape

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// classify returns whether the string is empty, contains single quotes, or contains special characters.
func classify(s string) (bool, bool, bool) {
	if len(s) == 0 {
		return true, false, false
	}
	var singleQ, special bool
	for _, r := range s {
		switch r {
		case '\'':
			singleQ = true
		case ' ', '\t', '\n', '\r', '\f', '\v', '$', '&', '"', '|', ';', '<', '>', '(', ')', '*', '?', '[', ']', '#', '~', '%', '!', '{', '}', '\\':
			special = true
		}
		if singleQ && special {
			// exit early if both conditions are met already
			return false, true, true
		}
	}
	return false, singleQ, special
}

// wrap in single quotes without escaping.
func wrapTo(str string, builder *strings.Builder) {
	builder.Grow(len(str) + 2)
	builder.WriteByte('\'')
	builder.WriteString(str)
	builder.WriteByte('\'')
}

// wrap in single quotes and escape single quotes and backslashes.
func escapeTo(str string, builder *strings.Builder) {
	builder.Grow(len(str) + 10)
	builder.WriteByte('\'')
	for _, c := range str { //nolint:varnamelen
		if c == '\'' {
			// quoting single quotes requires 4 extra chars, assume there's a closing quote too
			builder.Grow(10)
			builder.WriteString(`'"'"'`)
			continue
		}
		// According to strings.Map source code, this is faster than
		// always using WriteRune.
		if c < utf8.RuneSelf {
			builder.WriteByte(byte(c))
		} else {
			builder.WriteRune(c)
		}
	}
	builder.WriteByte('\'')
}

// Quote safely encloses a string in single quotes for shell usage.
func Quote(str string) string {
	empty, singleQ, special := classify(str)
	if empty {
		return "''"
	}
	if !singleQ && !special {
		return str
	}

	builder, ok := builderPool.Get().(*strings.Builder)
	if !ok {
		builder = &strings.Builder{}
	}
	defer builderPool.Put(builder)
	defer builder.Reset()

	if special && !singleQ {
		wrapTo(str, builder)
	} else {
		escapeTo(str, builder)
	}
	return builder.String()
}

// Join safely quotes and joins a list of strings for shell usage.
func Join(args ...string) string { //nolint:cyclop
	switch len(args) {
	case 0:
		return ""
	case 1:
		return Quote(args[0])
	}

	builder, ok := builderPool.Get().(*strings.Builder)
	if !ok {
		builder = &strings.Builder{}
	}
	defer builderPool.Put(builder)
	defer builder.Reset()

	var size int
	for _, arg := range args {
		size += len(arg)
	}
	size += len(args) - 1 // for spaces
	builder.Grow(size*2) // reserve space for escapes.
	for i, arg := range args {
		empty, singleQ, special := classify(arg)
		switch {
		case empty:
			builder.WriteString("''")
		case !singleQ && !special:
			builder.WriteString(arg)
		case special && !singleQ:
			escapeTo(arg, builder)
		default:
			wrapTo(arg, builder)
		}
		if i < len(args)-1 {
			builder.WriteByte(' ')
		}
	}
	return builder.String()
}

// QuoteCommand safely quotes and joins a list of strings for use as a shell command.
func QuoteCommand(args []string) string {
	return Join(args...)
}

func isPrint(r rune) rune {
	if unicode.IsPrint(r) {
		return r
	}

	return -1
}

// StripUnsafe removes non-printable runes from a string.
func StripUnsafe(s string) string {
	for _, r := range s {
		if isPrint(r) == -1 {
			// Avoid allocations by only stripping when the string contains non-printable runes.
			return strings.Map(isPrint, s)
		}
	}
	return s
}
