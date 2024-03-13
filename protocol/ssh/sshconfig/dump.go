package sshconfig

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
)

const indentLevel = 4

// Dump returns a string representation of the given ssh config object.
func Dump(obj withRequiredFields) (string, error) {
	fields, err := objFields(obj)
	if err != nil {
		return "", fmt.Errorf("dump config: failed to get fields: %w", err)
	}
	host, ok := fields[fkHost]
	if !ok {
		return "", fmt.Errorf("%w: dump config: missing required field: host", ErrInvalidObject)
	}
	builder := strings.Builder{}
	builder.WriteString("Host ")
	builder.WriteString(host.String())
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
		if capKey, ok := CapitalizeKey(key); ok {
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
