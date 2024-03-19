package sshconfig

import (
	"errors"
	"strings"
	"sync"
)

var (
	builderPool = sync.Pool{
		New: func() interface{} {
			return &strings.Builder{}
		},
	}

	// errMismatchedQuotes is returned when the input string has mismatched quotes when unquoting.
	errMismatchedQuotes = errors.New("mismatched quotes")
)

// SplitArgs splits a string into arguments like argv_split in ssh C source.
func SplitArgs(input string, terminateOnComment bool) ([]string, error) {
	var args []string

	argBuilder, ok := builderPool.Get().(*strings.Builder)
	if !ok {
		argBuilder = &strings.Builder{}
	}
	defer func() {
		argBuilder.Reset()
		builderPool.Put(argBuilder)
	}()
	argBuilder.Grow(len(input))

	var inQuote rune

	for i := 0; i < len(input); i++ {
		ch := rune(input[i])

		if terminateOnComment && ch == '#' {
			break
		}

		// Skip leading whitespace
		if inQuote == 0 && (ch == ' ' || ch == '\t') {
			if argBuilder.Len() > 0 {
				args = append(args, argBuilder.String())
				argBuilder.Reset()
			}
			continue
		}

		// Handle escape sequences
		if ch == '\\' {
			if i+1 < len(input) && (input[i+1] == '\\' || input[i+1] == '\'' || input[i+1] == '"' || (inQuote == 0 && input[i+1] == ' ')) {
				i++
				argBuilder.WriteRune(rune(input[i]))
			} else {
				argBuilder.WriteRune(ch)
			}
			continue
		}

		// Handle quotes
		if ch == '\'' || ch == '"' {
			if inQuote == ch {
				inQuote = 0
			} else if inQuote == 0 {
				inQuote = ch
				continue
			}
		}

		argBuilder.WriteRune(ch)

		// Check if arg is complete
		if inQuote == 0 && (ch == ' ' || ch == '\t') {
			// Remove trailing space
			arg := argBuilder.String()
			if len(arg) > 0 {
				arg = arg[:len(arg)-1]
				args = append(args, arg)
				argBuilder.Reset()
			}
		}
	}

	// Add the last arg
	if argBuilder.Len() > 0 {
		args = append(args, argBuilder.String())
	}
	if inQuote != 0 {
		return nil, errMismatchedQuotes
	}
	return args, nil
}
