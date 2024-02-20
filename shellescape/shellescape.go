// Package shellescape provides functions to escape strings for use in posix shell commands.
//
// It is a drop-in replacement for gopkg.in/alessio/shellescape.v1 except for StripUnsafe.
// There are no regular expressions and it avoids allocations by using a strings.Builder.
package shellescape

import (
	"strings"
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
		case ' ', '\t', '\n', '\r', '\f', '\v', '$', '&', '"', '|', ';', '<', '>', '(', ')', '*', '?', '[', ']', '#', '~', '%', '!':
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
	builder.Grow(len(str) + 3) // there will be at least 1 extra char and 2 quotes
	builder.WriteByte('\'')
	for _, c := range str {
		switch c {
		case '\'':
			builder.WriteString(`'"'"'`)
			continue
		case '\\':
			builder.WriteByte('\\')
		}
		builder.WriteRune(c)
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

	b := strings.Builder{}
	if special && !singleQ {
		wrapTo(str, &b)
	} else {
		escapeTo(str, &b)
	}
	return b.String()
}

// Join safely quotes and joins a list of strings for shell usage.
func Join(args ...string) string {
	switch len(args) {
	case 0:
		return ""
	case 1:
		return Quote(args[0])
	}

	builder := strings.Builder{}
	var size int
	for _, arg := range args {
		size += len(arg)
	}
	size += len(args) - 1 // for spaces
	builder.Grow(size)
	for i, arg := range args {
		empty, singleQ, special := classify(arg)
		switch {
		case empty:
			builder.WriteString("''")
		case !singleQ && !special:
			builder.WriteString(arg)
		case special && !singleQ:
			escapeTo(arg, &builder)
		default:
			wrapTo(arg, &builder)
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
