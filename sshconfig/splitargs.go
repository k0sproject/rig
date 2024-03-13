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

// splitArgs splits a string into arguments like argv_split in ssh C source. It is in fact almost a direct
// copy of the original function.
func splitArgs(input string, terminateOnComment bool) ([]string, error) { //nolint:cyclop
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

	var quote rune

	for i := 0; i < len(input); i++ {
		currCh := rune(input[i])

		// Skip leading whitespace
		if currCh == ' ' || currCh == '\t' {
			continue
		}

		if terminateOnComment && currCh == '#' {
			break
		}

		// Copy the token in, removing escapes
		for ; i < len(input); i++ { // weird stuff originates from the C sources
			currCh = rune(input[i])
			if currCh == '\\' { //nolint:gocritic,nestif
				if i+1 < len(input) && (input[i+1] == '\\' || input[i+1] == '\'' || input[i+1] == '"' || (quote == 0 && input[i+1] == ' ')) {
					i++ // Skip '\'
				}
				argBuilder.WriteRune(rune(input[i]))
			} else if quote == 0 && currCh == ' ' || currCh == '\t' {
				// done
				break
			} else if quote == 0 && (currCh == '"' || currCh == '\'') {
				quote = currCh
			} else if quote != 0 && currCh == quote {
				quote = 0 // quote end
			} else {
				argBuilder.WriteRune(currCh)
			}
		}
		if quote != 0 {
			return nil, errMismatchedQuotes
		}
		if argBuilder.Len() > 0 {
			args = append(args, argBuilder.String())
			argBuilder.Reset()
		}
	}
	return args, nil
}
