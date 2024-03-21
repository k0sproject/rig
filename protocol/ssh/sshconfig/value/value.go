// Package value defines value types for ssh configuration.
package value

import (
	"errors"
	"os"
	"sync"
)

var (
	home = sync.OnceValue(
		func() string {
			if home, err := os.UserHomeDir(); err == nil {
				return home
			}
			return ""
		},
	)

	// ErrInvalidValue is returned when trying to set an invalid value.
	ErrInvalidValue = errors.New("invalid value")
)

// Value is a generic type for a configuration value. It is necessary to track the origin of the value
// to be able to determine if it should be overridden by a new value and to resolve relative paths.
type Value[T any] struct {
	value  T
	origin string
	isSet  bool
}

// Set the value and its origin.
func (cv *Value[T]) Set(value T, origin string) {
	// if the value is already set and the origin is not defaults, don't override it
	if cv.IsSet() {
		return
	}
	cv.isSet = true
	cv.value = value
	cv.origin = origin
}

// IsSet returns true if the value is set.
func (cv Value[T]) IsSet() bool {
	return cv.isSet
}

// Get returns the value and a boolean indicating if the value was set.
func (cv Value[T]) Get() (T, bool) {
	return cv.value, cv.IsSet()
}

// Origin returns the origin of the value.
func (cv Value[T]) Origin() string {
	return cv.origin
}

// IsDefault returns true if the value is set from the defaults.
func (cv Value[T]) IsDefault() bool {
	return cv.origin == "default"
}
