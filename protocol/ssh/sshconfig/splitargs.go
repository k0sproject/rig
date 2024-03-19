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
func SplitArgs(input string, terminateOnComment bool) ([]string, error) { //nolint:gocognit,cyclop
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
		currCh := rune(input[i])

		if terminateOnComment && currCh == '#' {
			break
		}

		// Skip leading whitespace
		if inQuote == 0 && (currCh == ' ' || currCh == '\t') {
			if argBuilder.Len() > 0 {
				args = append(args, argBuilder.String())
				argBuilder.Reset()
			}
			continue
		}

		// Handle escape sequences
		if currCh == '\\' {
			if i+1 < len(input) && (input[i+1] == '\\' || input[i+1] == '\'' || input[i+1] == '"' || (inQuote == 0 && input[i+1] == ' ')) {
				i++
				argBuilder.WriteRune(rune(input[i]))
			} else {
				argBuilder.WriteRune(currCh)
			}
			continue
		}

		// Handle quotes
		if currCh == '\'' || currCh == '"' {
			if inQuote == currCh {
				inQuote = 0
			} else if inQuote == 0 {
				inQuote = currCh
				continue
			}
		}

		argBuilder.WriteRune(currCh)

		// Check if arg is complete
		if inQuote == 0 && (currCh == ' ' || currCh == '\t') {
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
