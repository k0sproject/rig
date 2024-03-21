package sshconfig

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
)

const indentLevel = 4

// Dump returns a string representation of the given ssh config object.
func Dump(obj HostConfig) (string, error) {
	fields, err := objFields(obj)
	if err != nil {
		return "", fmt.Errorf("dump config: failed to get fields: %w", err)
	}
	host, ok := obj.GetHost()
	if !ok {
		return "", fmt.Errorf("%w: dump config: missing host from object", ErrInvalidObject)
	}
	builder := strings.Builder{}
	builder.WriteString("Host ")
	builder.WriteString(host)
	builder.WriteByte('\n')

	indent := bytes.Repeat([]byte(" "), indentLevel)

	keys := make([]string, 0, len(fields)-1)
	for key := range fields {
		if key != fkHost {
			keys = append(keys, key)
		}
	}

	sort.Strings(keys)

	for _, key := range keys {
		field, ok := fields[key]
		if !ok {
			return "", fmt.Errorf("%w: dump config: mysteriously missing field: %s", ErrInvalidObject, key)
		}
		if !field.IsSet() {
			continue
		}
		if capKey, ok := capitalizeKey(key); ok {
			key = capKey
		}
		builder.Write(indent)
		builder.WriteString(key)
		builder.WriteByte(' ')
		builder.WriteString(field.String())
		builder.WriteByte('\n')
	}
	return builder.String(), nil
}

// DumpG returns a string representation of the given ssh config object in the same format as
// you get from `ssh -G`. This is useful for debugging and testing.
func DumpG(obj HostConfig) (string, error) {
	fields, err := objFields(obj)
	if err != nil {
		return "", fmt.Errorf("dump config: failed to get fields: %w", err)
	}

	builder := strings.Builder{}

	keys := make([]string, 0, len(fields)-1)
	for key := range fields {
		if key != fkHost {
			keys = append(keys, key)
		}
	}

	sort.Strings(keys)

	for _, key := range keys {
		field, ok := fields[key]
		if !ok {
			return "", fmt.Errorf("%w: dump config: mysteriously missing field: %s", ErrInvalidObject, key)
		}
		if !field.IsSet() {
			continue
		}
		builder.WriteString(key)
		builder.WriteByte(' ')
		builder.WriteString(field.String())
		builder.WriteByte('\n')
	}
	return builder.String(), nil
}
