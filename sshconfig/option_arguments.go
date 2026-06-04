package sshconfig

import (
	"fmt"
	"maps"
	"strings"
)

// OptionArguments holds ssh_config options as key-value pairs. Values may be
// strings, booleans, integers, or other fmt-printable types. Booleans are
// rendered as "yes"/"no" when converted to command-line arguments or applied
// to a Setter. Setting a key to nil deletes it.
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

// IsSet reports whether the option has been set.
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

// sliceToStrings converts a []string or []any value to a []string, returning
// (nil, false) if v is not a slice type.
func sliceToStrings(v any) ([]string, bool) {
	switch tv := v.(type) {
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
		return nil, false
	}
}

// ToArgs converts the options to a list of "-o Key=Value" command-line arguments
// suitable for passing to the openssh binary. Slice values are rendered as
// space-separated tokens (e.g. []string{"/a", "/b"} → "Key=/a /b").
func (o OptionArguments) ToArgs() []string {
	args := make([]string, 0, len(o)*2)
	for key, val := range o {
		if val == nil {
			continue
		}
		if strs, ok := sliceToStrings(val); ok {
			args = append(args, "-o", key+"="+strings.Join(strs, " "))
			continue
		}
		args = append(args, "-o", key+"="+valToString(val))
	}
	return args
}

// ApplyTo feeds each option into setter using [Setter.Set]. Booleans are
// converted to "yes"/"no"; slice values are expanded into variadic arguments
// so multi-valued directives (IdentityFile, SendEnv, etc.) work correctly.
// Returns an error if the setter rejects a value (e.g. bad format, or unknown
// key when [Setter.ErrorOnUnknownFields] is set).
func (o OptionArguments) ApplyTo(setter *Setter) error {
	if setter == nil {
		return nil
	}
	for key, v := range o {
		if v == nil {
			continue
		}
		var err error
		if strs, ok := sliceToStrings(v); ok {
			err = setter.Set(key, strs...)
		} else {
			err = setter.Set(key, valToString(v))
		}
		if err != nil {
			return fmt.Errorf("ssh option %q: %w", key, err)
		}
	}
	return nil
}
