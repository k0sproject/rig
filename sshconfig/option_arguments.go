package sshconfig

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"strings"
)

// ErrEmptySlice is returned when a slice option value is empty.
var ErrEmptySlice = errors.New("empty slice is not a valid value")

// OptionArguments holds ssh_config options as key-value pairs. Values may be
// strings, booleans, integers, or other fmt-printable types. Booleans are
// rendered as "yes"/"no" when converted to command-line arguments or applied
// to a Setter. Use [OptionArguments.Set] with a nil value to delete a key.
//
// It is used by both the OpenSSH and the pure-Go SSH protocol implementations:
// the OpenSSH path renders options as "-o Key=Value" arguments via [OptionArguments.ToArgs];
// the pure-Go path applies them directly to a [Setter] via [OptionArguments.ApplyTo].
type OptionArguments map[string]any

// Copy returns a shallow copy of the options.
func (o OptionArguments) Copy() OptionArguments {
	dup := make(OptionArguments, len(o))
	maps.Copy(dup, o)
	return dup
}

// Set sets an option key to value. A nil value deletes the key.
func (o OptionArguments) Set(key string, value any) {
	if value == nil {
		delete(o, key)
		return
	}
	o[key] = value
}

// SetIfUnset sets the option only if it has not already been set.
func (o OptionArguments) SetIfUnset(key string, value any) {
	if o.IsSet(key) {
		return
	}
	o.Set(key, value)
}

// IsSet reports whether the option key is present in the map.
// A key with a nil value (e.g. from YAML "key: null") is treated as set,
// so that nil can act as an explicit delete sentinel that prevents
// [OptionArguments.SetIfUnset] from applying defaults. Use [OptionArguments.Set]
// with a nil value to remove a key entirely (making it unset again).
func (o OptionArguments) IsSet(key string) bool {
	_, ok := o[key]
	return ok
}

// valToString converts a single ssh_config value to its string representation.
// Booleans become "yes"/"no"; all other types use fmt.Sprint.
func valToString(v any) string {
	if b, ok := v.(bool); ok {
		if b {
			return "yes"
		}
		return "no"
	}
	return fmt.Sprint(v)
}

// sliceToStrings converts a slice value to a []string, returning (nil, false)
// if val is not a slice type. Handles []string, []any, and any other slice type
// by reflecting over the elements and converting each via [valToString].
func sliceToStrings(val any) ([]string, bool) {
	switch tv := val.(type) {
	case []string:
		out := make([]string, len(tv))
		copy(out, tv)
		return out, true
	case []any:
		out := make([]string, len(tv))
		for i, elem := range tv {
			out[i] = valToString(elem)
		}
		return out, true
	default:
		rv := reflect.ValueOf(val)
		if rv.Kind() != reflect.Slice {
			return nil, false
		}
		out := make([]string, rv.Len())
		for i := range rv.Len() {
			out[i] = valToString(rv.Index(i).Interface())
		}
		return out, true
	}
}

// ToArgs converts the options to a list of "-o Key=Value" command-line arguments
// suitable for passing to the openssh binary. Slice values are rendered as
// space-separated tokens for space-delimited directives (e.g. IdentityFile) and
// comma-separated tokens for CSV directives (e.g. Ciphers, KexAlgorithms, MACs).
func (o OptionArguments) ToArgs() []string {
	args := make([]string, 0, len(o)*2)
	for key, val := range o {
		if val == nil {
			continue
		}
		if strs, ok := sliceToStrings(val); ok {
			if len(strs) == 0 {
				continue
			}
			sep := " "
			if isCSVKey(key) {
				sep = ","
			}
			args = append(args, "-o", key+"="+strings.Join(strs, sep))
			continue
		}
		args = append(args, "-o", key+"="+valToString(val))
	}
	return args
}

// ApplyTo feeds each option into setter using [Setter.Set]. Booleans are
// converted to "yes"/"no"; for space-delimited directives (e.g. IdentityFile,
// SendEnv) slice values are expanded into variadic arguments; for CSV
// directives (e.g. Ciphers, KexAlgorithms, MACs) slice values are joined with
// commas and passed as a single argument, matching what the setter expects.
// Returns an error if the setter rejects a value (e.g. bad format, or unknown
// key when [Setter.ErrorOnUnknownFields] is set).
func (o OptionArguments) ApplyTo(setter *Setter) error {
	if setter == nil {
		return nil
	}
	for key, val := range o {
		if val == nil {
			continue
		}
		var err error
		if strs, ok := sliceToStrings(val); ok {
			if len(strs) == 0 {
				return fmt.Errorf("ssh option %q: %w", key, ErrEmptySlice)
			}
			if isCSVKey(key) {
				err = setter.Set(key, strings.Join(strs, ","))
			} else {
				err = setter.Set(key, strs...)
			}
		} else {
			err = setter.Set(key, valToString(val))
		}
		if err != nil {
			return fmt.Errorf("ssh option %q: %w", key, err)
		}
	}
	return nil
}
