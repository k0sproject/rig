// Package shellescape provides functions to escape strings for use in posix shell commands.
//
// It is a drop-in replacement for gopkg.in/alessio/shellescape.v1.
//
// Additionally an Unquote function is provided.
package shellescape

import (
	"strings"
	"unicode"
)

// classify returns whether the string is empty, contains single quotes, or contains special characters.
//
// The set of special characters covers everything the reference implementation
// this package replaces treats as unsafe - anything outside [\w@%+=:,./-] - plus
// the percent sign, which that implementation considers safe but fish uses for
// process expansion. Backtick and caret used to be missing from the set, which
// meant a string like "`id`" came back unquoted for a shell to substitute.
func classify(s string) (bool, bool, bool) {
	if len(s) == 0 {
		return true, false, false
	}
	var singleQ, special bool
	for _, r := range s {
		switch r {
		case '\'':
			singleQ = true
		case ' ', '\t', '\n', '\r', '\f', '\v', '$', '&', '"', '|', ';', '<', '>', '(', ')', '*', '?', '[', ']', '#', '~', '%', '!', '{', '}', '\\', '`', '^':
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

// wrap in single quotes and escape single quotes.
//
// Bytes are copied as they are rather than decoded into runes: a file name on a
// POSIX host is a byte string that need not be valid UTF-8, and ranging over
// runes would rewrite every invalid byte into U+FFFD, quoting a name that is not
// the one asked for. The characters that need escaping are all ASCII, and UTF-8
// never encodes those as part of a multi-byte sequence.
func escapeTo(str string, builder *strings.Builder) {
	builder.Grow(len(str) + 10)
	builder.WriteByte('\'')
	for i := range len(str) {
		if str[i] == '\'' {
			// quoting single quotes requires 4 extra chars, assume there's a closing quote too
			builder.Grow(10)
			builder.WriteString(`'"'"'`)
			continue
		}
		builder.WriteByte(str[i])
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

// escapeForLoginShellTo wraps str in single quotes, escaping single quotes and
// backslashes outside of the quoted runs, where every supported shell agrees on
// their meaning. Bytes are copied as they are, for the reason given on [escapeTo].
func escapeForLoginShellTo(str string, builder *strings.Builder) {
	builder.Grow(len(str) + 10)
	builder.WriteByte('\'')
	for i := range len(str) {
		switch str[i] {
		case '\'':
			builder.Grow(10)
			builder.WriteString(`'"'"'`)
		case '\\':
			// Leave the quoted run, write an escaped backslash, and resume it.
			// Both POSIX shells and fish read \\ outside quotes as one
			// backslash, while inside single quotes they disagree.
			builder.Grow(10)
			builder.WriteString(`'\\'`)
		default:
			builder.WriteByte(str[i])
		}
	}
	builder.WriteByte('\'')
}

// QuoteForLoginShell safely encloses a string in single quotes for a shell that
// the program did not choose: in practice the remote user's login shell, which
// sshd hands the command line to before anything else sees it.
//
// Use it for that boundary and nothing else. [Quote] is correct, shorter and
// easier to read for every layer inside it, because those are parsed by the
// POSIX shell that rig imposes on the command (see cmd.Executor.SetShell) rather
// than by whatever the remote user's shell happens to be.
//
// The difference is backslashes. fish reads \\ and \' as escapes inside single
// quotes, where POSIX shells pass them through verbatim, so a POSIX-quoted
// 'a\\b' reaches the imposed shell as a\b. Wrapping a command in a shell
// invocation is therefore not by itself enough to protect it: the darwin OS
// release detection sends a sed expression carrying consecutive backslashes, and
// under a fish login shell that expression silently changed meaning. This
// function escapes single quotes and backslashes outside the quoted runs
// instead, where both shells agree that \\ means one backslash.
//
// The csh family is not covered: it expands ! before it considers quoting, and
// takes no embedded newline inside quotes at all. Both fail loudly rather than
// changing the command.
func QuoteForLoginShell(str string) string {
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

	if !singleQ && !strings.ContainsRune(str, '\\') {
		wrapTo(str, builder)
	} else {
		escapeForLoginShellTo(str, builder)
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

	size += len(args) - 1  // for spaces
	builder.Grow(size * 2) // reserve space for escapes.

	for i, arg := range args {
		empty, singleQ, special := classify(arg)
		switch {
		case empty:
			builder.WriteString("''")
		case !singleQ && !special:
			builder.WriteString(arg)
		case special && !singleQ:
			wrapTo(arg, builder)
		default:
			escapeTo(arg, builder)
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
