package shellescape

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

	// ErrMismatchedQuotes is returned when the input string has mismatched quotes when unquoting.
	ErrMismatchedQuotes = errors.New("mismatched quotes")
)

// Unquote is a mostly POSIX compliant implementation of unquoting a string the same way a shell would.
// Variables and command substitutions are not handled.
func Unquote(input string) (string, error) { //nolint:cyclop
	sb, ok := builderPool.Get().(*strings.Builder)
	if !ok {
		sb = &strings.Builder{}
	}
	defer builderPool.Put(sb)
	defer sb.Reset()

	var inDoubleQuotes, inSingleQuotes, isEscaped bool

	for i := 0; i < len(input); i++ {
		currentChar := input[i]

		if isEscaped {
			sb.WriteByte(currentChar)
			isEscaped = false
			continue
		}

		switch currentChar {
		case '\\':
			if !inSingleQuotes { // Escape works in double quotes or outside any quotes
				isEscaped = true
			} else {
				sb.WriteByte(currentChar) // Treat as a regular character within single quotes
			}
		case '"':
			if !inSingleQuotes { // Toggle double quotes only if not in single quotes
				inDoubleQuotes = !inDoubleQuotes
			} else {
				sb.WriteByte(currentChar) // Treat as a regular character within single quotes
			}
		case '\'':
			if !inDoubleQuotes { // Toggle single quotes only if not in double quotes
				inSingleQuotes = !inSingleQuotes
			} else {
				sb.WriteByte(currentChar) // Treat as a regular character within double quotes
			}
		default:
			sb.WriteByte(currentChar)
		}
	}

	if inDoubleQuotes || inSingleQuotes {
		return "", fmt.Errorf("unquote `%q`: %w", input, ErrMismatchedQuotes)
	}

	return sb.String(), nil
}
