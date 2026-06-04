package sshconfig

import (
	"fmt"
	"maps"
)

// OptionArguments holds ssh_config options as key-value pairs. Values may be
// strings, booleans, or integers; booleans are rendered as "yes"/"no" when
// converted to command-line arguments or applied to a Setter.
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

// Set sets an option key to value.
func (o OptionArguments) Set(key string, value any) {
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

// ToArgs converts the options to a list of "-o Key=Value" command-line arguments
// suitable for passing to the openssh binary.
func (o OptionArguments) ToArgs() []string {
	args := make([]string, 0, len(o)*2)
	for k, v := range o {
		if b, ok := v.(bool); ok {
			if b {
				args = append(args, "-o", k+"=yes")
			} else {
				args = append(args, "-o", k+"=no")
			}
			continue
		}
		args = append(args, "-o", fmt.Sprintf("%s=%v", k, v))
	}
	return args
}

// ApplyTo feeds each option into setter using [Setter.Set]. Booleans are
// converted to "yes"/"no"; all other values are formatted with fmt.Sprint.
// Returns an error if the setter rejects a value (e.g. bad format, or unknown
// key when [Setter.ErrorOnUnknownFields] is set).
func (o OptionArguments) ApplyTo(setter *Setter) error {
	for key, v := range o {
		var strVal string
		switch tv := v.(type) {
		case bool:
			if tv {
				strVal = "yes"
			} else {
				strVal = "no"
			}
		case string:
			strVal = tv
		default:
			strVal = fmt.Sprint(tv)
		}
		if err := setter.Set(key, strVal); err != nil {
			return fmt.Errorf("ssh option %q: %w", key, err)
		}
	}
	return nil
}
