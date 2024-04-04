package sshconfig

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// This file defines custom setter function builders.

// preloadedDefaultsSetter returns a setter for the algo/cipher fields where prefixes can be used
// to modify defaults.
func preloadedDefaultsSetter(defaultValues string) setterFunc { //nolint:cyclop
	defaults := strings.Split(defaultValues, ",")

	return func(s *Setter, key string, values ...string) error {
		if err := expectOne(values); err != nil {
			return fmt.Errorf("%w: expected comma separated values, got %q", errInvalidValue, values)
		}
		value := values[0]
		field, err := s.get(key, reflect.Slice, reflect.String)
		if err != nil {
			return err
		}
		if field.Len() > 0 {
			// the precedence in these fields is standard.
			// the modifiers only work when the field is empty.
			// the modifiers work ONCE against the default values.
			// for example, something like -aes128 will apply the defaults minus aes128
			// and after that ^aes256 or +aes128 won't do anything.
			return nil
		}
		var prefix string
		switch value[0] {
		case '+':
			prefix = "+"
		case '-':
			prefix = "-"
		case '^':
			prefix = "^"
		}
		if prefix == "" {
			// no modifier, just set the values
			field.Set(reflect.ValueOf(strings.Split(value, ",")))
			return nil
		}
		values = strings.Split(value[1:], ",")
		var newValues []string
		switch prefix {
		case "+":
			// + prefix appends to the defaults list
			newValues = append(newValues, defaults...)
			for _, v := range values {
				if !slices.Contains(newValues, v) {
					newValues = append(newValues, v)
				}
			}
		case "^":
			// ^ prefix prepends (or shifts) the values to the beginning of defaults list
			newValues = append(newValues, values...)
			for _, v := range defaults {
				if !slices.Contains(newValues, v) {
					newValues = append(newValues, v)
				}
			}
		case "-":
			// - prefix removes matching values from the defaults list
		defaultFor:
			for _, v := range defaults {
				for _, value := range values {
					matches, err := patternMatch(value, v)
					if err != nil {
						return fmt.Errorf("%w: invalid pattern %q: %w", errInvalidValue, value, err)
					}
					if matches {
						continue defaultFor
					}
					newValues = append(newValues, v)
				}
			}
		}
		field.Set(reflect.ValueOf(newValues))
		return nil
	}
}

// enum returns a setter that can set a field to one of the predefined values.
func enum(states ...string) setterFunc {
	return func(s *Setter, key string, values ...string) error {
		if err := expectOne(values); err != nil {
			return err
		}
		if !slices.Contains(states, values[0]) {
			return fmt.Errorf("%w: invalid value %q: expected one of %q", errInvalidValue, values[0], states)
		}
		return s.setString(key, values[0])
	}
}

type normalizeable[T any] interface {
	Normalize() (T, error)
	String() string
}

// returns a setter for the bool-like fields in options/options.go.
func extendedBoolSetter[T normalizeable[T]]() setterFunc {
	return func(s *Setter, key string, values ...string) error { //nolint:varnamelen
		if err := expectOne(values); err != nil {
			return err
		}

		var extbool T
		extboolVal := reflect.ValueOf(&extbool).Elem()
		extboolVal.Set(reflect.ValueOf(values[0]).Convert(extboolVal.Type()))
		normalized, err := extbool.Normalize()
		if err != nil {
			return fmt.Errorf("not a valid %q value %q: %w", key, values[0], err)
		}

		value := reflect.ValueOf(normalized)
		field, err := s.get(key, value.Kind())
		if err != nil {
			if f, err := s.get(key, reflect.String); err == nil {
				if f.String() != "" {
					return nil
				}
				f.SetString(normalized.String())
				return nil
			}
			return fmt.Errorf("%w: field %q is not a %s or a string", errInvalidField, key, reflect.TypeOf(normalized))
		}
		if field.String() != "" {
			return nil
		}
		field.Set(value)
		return nil
	}
}
