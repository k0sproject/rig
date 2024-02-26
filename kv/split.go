// Package kv is for working with key-value pairs often found in configuration files.
package kv

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

var (
	builderPool = sync.Pool{
		New: func() interface{} {
			return &strings.Builder{}
		},
	}

	// ErrSyntax is returned when the input string has a syntactical error.
	ErrSyntax = errors.New("syntax error")
)

// SplitRune splits a string into a key and a value using the given separator. Quotes and escape characters are parsed in a similar manner to a shell.
// The separator is not included in the key or value.
// If the input string has a syntax error such as mismatched quotes or no delimiter, the error will be of type kv.ErrSyntax.
func SplitRune(s string, separator rune) (key string, value string, err error) { //nolint:cyclop
	var inSingleQuotes, inDoubleQuotes, isEscaped bool

	sb, ok := builderPool.Get().(*strings.Builder)
	if !ok {
		sb = &strings.Builder{}
	}
	defer builderPool.Put(sb)
	defer sb.Reset()

	for _, currentChar := range s {
		switch currentChar {
		case '\\':
			if !inSingleQuotes && !isEscaped {
				isEscaped = true
			} else {
				sb.WriteRune(currentChar)
				isEscaped = false
			}
		case '"':
			if !inSingleQuotes {
				if isEscaped {
					sb.WriteRune(currentChar)
					isEscaped = false
				} else {
					inDoubleQuotes = !inDoubleQuotes
				}
			} else {
				sb.WriteRune(currentChar)
			}
		case '\'':
			if !inDoubleQuotes && !isEscaped {
				inSingleQuotes = !inSingleQuotes
			} else {
				sb.WriteRune(currentChar)
				isEscaped = false
			}
		case separator:
			if !inSingleQuotes && !inDoubleQuotes && !isEscaped {
				key = sb.String()
				sb.Reset()
				sb.Grow(len(s) - len(key))
			}
			isEscaped = false
		default:
			isEscaped = false
			sb.WriteRune(currentChar)
		}
	}

	if inSingleQuotes || inDoubleQuotes {
		return "", "", fmt.Errorf("%w: mismatched quotes in %q", ErrSyntax, s)
	}

	if len(key) == 0 {
		return "", "", fmt.Errorf("%w: no separator found in %q", ErrSyntax, s)
	}
	return key, sb.String(), nil
}

// Split is a convenience function that calls SplitRune with '=' as the separator.
func Split(s string) (key string, value string, err error) {
	return SplitRune(s, '=')
}
