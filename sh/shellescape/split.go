package shellescape

import (
	"fmt"
	"strings"
)

// Split splits the input string respecting shell-like quoted segments.
func Split(input string) ([]string, error) { //nolint:cyclop
	var segments []string

	currentSegment, ok := builderPool.Get().(*strings.Builder)
	if !ok {
		currentSegment = &strings.Builder{}
	}
	defer builderPool.Put(currentSegment)
	defer currentSegment.Reset()

	// inSegment lets "" produce an empty argument while repeated spaces don't.
	var inDoubleQuotes, inSingleQuotes, isEscaped, inSegment bool

	for i := range len(input) {
		currentChar := input[i]

		if isEscaped {
			currentSegment.WriteByte(currentChar)
			isEscaped = false
			continue
		}

		switch {
		case currentChar == '\\' && !inSingleQuotes:
			isEscaped = true
			inSegment = true
		case currentChar == '"' && !inSingleQuotes:
			inDoubleQuotes = !inDoubleQuotes
			inSegment = true
		case currentChar == '\'' && !inDoubleQuotes:
			inSingleQuotes = !inSingleQuotes
			inSegment = true
		case currentChar == ' ' && !inDoubleQuotes && !inSingleQuotes:
			// Space outside quotes; delimiter for a new segment
			if inSegment {
				segments = append(segments, currentSegment.String())
				currentSegment.Reset()
				inSegment = false
			}
		default:
			currentSegment.WriteByte(currentChar)
			inSegment = true
		}
	}

	if inDoubleQuotes || inSingleQuotes {
		return nil, fmt.Errorf("split `%q`: %w", input, ErrMismatchedQuotes)
	}

	if isEscaped {
		return nil, fmt.Errorf("split `%q`: %w", input, ErrTrailingBackslash)
	}

	// Add the last segment if present
	if inSegment {
		segments = append(segments, currentSegment.String())
	}

	return segments, nil
}
