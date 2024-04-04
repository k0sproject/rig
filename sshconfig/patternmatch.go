package sshconfig

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// patternMatch compares a single pattern against a string.
func patternMatch(value, pattern string) (bool, error) {
	if pattern == "*" {
		return true, nil
	}

	if pattern == value {
		return true, nil
	}

	if !strings.ContainsAny(pattern, "*?") {
		return pattern == value, nil
	}

	var sb strings.Builder
	sb.WriteString("^")
	for _, ch := range pattern {
		switch ch {
		case '*':
			sb.WriteString(".*")
		case '?':
			sb.WriteString(".")
		default:
			if !unicode.IsLetter(ch) && !unicode.IsNumber(ch) {
				sb.WriteRune('\\')
			}
			sb.WriteRune(ch)
		}
	}
	sb.WriteString("$")

	regex, err := regexp.Compile(sb.String())
	if err != nil {
		return false, fmt.Errorf("invalid pattern: %w", err)
	}

	return regex.MatchString(value), nil
}

// patternMatchAll returns true if the value matches the combination of
// multiple patterns.
//
// A !negated patterns alone will never yield a match unless there is also a positive
// match in the combination.
func patternMatchAll(value string, patterns ...string) (bool, error) {
	var hasPositiveMatch bool

	for _, pattern := range patterns {
		if pattern == "" {
			continue
		}
		subPatterns := strings.Split(pattern, ",")
		for _, subPattern := range subPatterns {
			subPattern = strings.TrimSpace(subPattern)
			if subPattern == "" {
				continue
			}
			negate := strings.HasPrefix(subPattern, "!")
			if negate {
				subPattern = subPattern[1:]
			}

			match, err := patternMatch(value, subPattern)
			if err != nil {
				return false, err
			}

			if match {
				if negate {
					return false, nil
				}
				hasPositiveMatch = true
			}
		}
	}

	return hasPositiveMatch, nil
}
