package sshconfig

import (
	"errors"
	"fmt"
	"math"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/k0sproject/rig/v2/homedir"
	"github.com/k0sproject/rig/v2/sh/shellescape"
)

var (
	home = sync.OnceValue(
		func() string {
			home, _ := homedir.Expand("~")
			return home
		},
	)
	username = sync.OnceValue(
		func() string {
			if user, err := user.Current(); err == nil {
				return user.Username
			}
			return ""
		},
	)

	// ErrInvalidValue is returned when trying to set an invalid value.
	ErrInvalidValue = errors.New("invalid value")
)

// ValueOriginType is an enum type for the origin of a configuration value.
type ValueOriginType int

const (
	// ValueOriginUnset indicates that the value is not set.
	ValueOriginUnset ValueOriginType = 0

	// ValueOriginDefault indicates that the value is set from the defaults.
	ValueOriginDefault ValueOriginType = 1

	// ValueOriginFile indicates that the value is set from a config file. The origin field should contain the file name.
	ValueOriginFile ValueOriginType = 2

	// ValueOriginOption indicates that the value is set manually.
	ValueOriginOption ValueOriginType = 3

	// ValueOriginCanonicalize indicates that the value is set from the canonicalization rules (should only be set on the HostName field).
	ValueOriginCanonicalize ValueOriginType = 4

	boolTrue  = "true"
	boolFalse = "false"
	boolYes   = "yes"
	boolNo    = "no"

	fkHost = "host"
)

// Value is a generic type for a configuration value. It is necessary to track the origin of the value
// to be able to determine if it should be overridden by a new value and to resolve relative paths.
type Value[T any] struct {
	value      T
	originType ValueOriginType
	origin     string
}

// Set the value and its origin.
func (cv *Value[T]) Set(value T, originType ValueOriginType, origin string) {
	// if the value is already set and the origin is not defaults, don't override it
	if cv.IsSet() && cv.originType != ValueOriginDefault {
		return
	}
	cv.value = value
	cv.originType = originType
	cv.origin = origin
}

// IsSet returns true if the value is set.
func (cv Value[T]) IsSet() bool {
	return cv.originType != ValueOriginUnset
}

// Get returns the value and a boolean indicating if the value was set.
func (cv Value[T]) Get() (T, bool) {
	return cv.value, cv.IsSet()
}

// OriginType returns the origin type of the value.
func (cv Value[T]) OriginType() ValueOriginType {
	return cv.originType
}

// Origin returns the origin of the value.
func (cv Value[T]) Origin() string {
	return cv.origin
}

// IsDefault returns true if the value is set from the defaults.
func (cv Value[T]) IsDefault() bool {
	return cv.originType == ValueOriginDefault
}

// StringValue is a configuration value that holds a string.
type StringValue struct {
	Value[string]
}

// SetString sets the value of the string and its origin.
func (v *StringValue) SetString(value string, originType ValueOriginType, origin string) error {
	v.Set(value, originType, origin)
	return nil
}

// String returns the value as a string.
func (v *StringValue) String() string {
	val, _ := v.Get()
	return val
}

// BoolValue is a configuration value that holds a boolean.
type BoolValue struct {
	Value[bool]
}

// SetString sets the value of the boolean and its origin. It accepts "yes", "true", "no" and "false" as valid values.
func (v *BoolValue) SetString(value string, originType ValueOriginType, origin string) error {
	switch value {
	case boolYes, boolTrue:
		v.Set(true, originType, origin)
	case boolNo, boolFalse:
		v.Set(false, originType, origin)
	default:
		return fmt.Errorf("%w: invalid value %q for a boolean field", ErrInvalidValue, value)
	}
	return nil
}

// String returns the value as a string.
func (v *BoolValue) String() string {
	val, _ := v.Get()
	if val {
		return boolYes
	}
	return boolNo
}

// MultiStateBoolValue is a configuration value that can be a boolean or a string. Fields like this are used in the
// ssh configuration for things like "yes/no/ask" or "yes/no/auto". The Bool() function returns the value as a boolean.
type MultiStateBoolValue struct {
	Value[string]
}

// SetString sets the value of the multi-state value and its origin. It accepts "yes", "true", "no" and "false" as boolean values.
func (v *MultiStateBoolValue) SetString(value string, originType ValueOriginType, origin string) error {
	v.Set(value, originType, origin)
	return nil
}

// Bool returns the value as a boolean. It returns the boolean value and a boolean indicating if the value was set to a boolean value.
// If the value is not set to a boolean value, the string can be retrieved using the Get() function.
func (v *MultiStateBoolValue) Bool() (bool, bool) {
	val, ok := v.Get()
	if !ok {
		return false, false
	}
	switch val {
	case boolYes, boolTrue:
		return true, true
	case boolNo, boolFalse:
		return false, true
	}
	return false, false
}

// String returns the value as a string.
func (v *MultiStateBoolValue) String() string {
	val, _ := v.Get()
	return val
}

// UintValue is a configuration value that holds an unsigned integer.
type UintValue struct {
	Value[uint]
}

// SetString sets the value of the unsigned integer and its origin.
func (v *UintValue) SetString(value string, originType ValueOriginType, origin string) error {
	num, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid uint value %q: %w", value, err)
	}
	if num > math.MaxUint {
		return fmt.Errorf("%w: uint value %q exceed maxuint", ErrInvalidValue, value)
	}
	v.Set(uint(num), originType, origin)
	return nil
}

// String returns the value as a string.
func (v *UintValue) String() string {
	val, _ := v.Get()
	return strconv.FormatUint(uint64(val), 10)
}

// DurationValue is a configuration value that holds a time.Duration. The ssh configuration uses the same format as the time package
// for duration, except it parses a number without a unit suffix to be seconds. 1m30s is a valid duration.
type DurationValue struct {
	Value[time.Duration]
}

// SetString sets the value of the duration and its origin.
func (v *DurationValue) SetString(value string, originType ValueOriginType, origin string) error {
	if value == "none" {
		v.Set(0, originType, origin)
		return nil
	}
	unit := value[len(value)-1]
	if unit >= '0' && unit <= '9' {
		value += "s"
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return fmt.Errorf("invalid duration value %q: %w", value, err)
	}
	v.Set(d, originType, origin)
	return nil
}

// String returns the value as a string.
func (v *DurationValue) String() string {
	val, _ := v.Get()
	return val.String()
}

// StringListValueis a configuration value that holds a slice of strings. When setting the value, it accepts a
// comma-separated or whitespace-separated list of values. The values can be quoted using single or double quotes.
// If the existing value is set from the defaults, the slice is cleared before setting the new value. Duplicate
// values are ignored.
type StringListValue struct {
	Value[[]string]
}

// SetString appends the value to the slice and sets the origin of the value. If the value is already set from any
// other origin than the defaults, it appends the new value to the slice. If the value is set from the defaults, it
// clears the slice before setting the new values.
func (v *StringListValue) SetString(value string, originType ValueOriginType, origin string) error {
	var oldVals []string
	if v.OriginType() != ValueOriginDefault {
		oldVals, _ = v.Get()
	}

	var newVals []string
	if strings.Contains(value, ",") {
		newVals = strings.Split(value, ",")
		for i, val := range newVals {
			v, err := shellescape.Unquote(strings.TrimSpace(val))
			if err != nil {
				return fmt.Errorf("can't parse string slice value %q: %w", value, err)
			}
			newVals[i] = v
		}
	} else {
		vals, err := shellescape.Split(value)
		if err != nil {
			return fmt.Errorf("can't parse string slice value %q: %w", value, err)
		}
		newVals = vals
	}
	for _, newVal := range newVals {
		if newVal == "" {
			continue
		}
		var exists bool
		for _, oldVal := range oldVals {
			if oldVal == newVal {
				exists = true
			}
		}
		if !exists {
			oldVals = append(oldVals, newVal)
		}
	}

	v.Set(oldVals, originType, origin)
	return nil
}

func formatStringSlice(vals []string, sep rune) string {
	var sb strings.Builder
	for i, val := range vals {
		if i > 0 {
			sb.WriteRune(sep)
		}
		if strings.Contains(val, " ") {
			sb.WriteString(strconv.Quote(val))
		} else {
			sb.WriteString(val)
		}
	}
	return sb.String()
}

// String returns the value as a string.
func (v *StringListValue) String() string {
	val, _ := v.Get()
	return formatStringSlice(val, ',')
}

// IntListValue is a configuration value that holds a slice of integers. When setting the value, it accepts a
// comma-separated or whitespace-separated list of values. The values can be quoted using single or double quotes.
// If the existing value is set from the defaults, the slice is cleared before setting the new value.
type IntListValue struct {
	Value[[]int]
}

// SetString appends the value to the slice and sets the origin of the value. If the value is set from the defaults, it
// clears the slice before setting the new values. Duplicate values are ignored.
func (v *IntListValue) SetString(value string, originType ValueOriginType, origin string) error {
	var oldVals []int
	if v.OriginType() != ValueOriginDefault {
		oldVals, _ = v.Get()
	}

	var newVals []int //nolint:prealloc // size is not known, the input can have blanks.
	var stringVals []string
	if strings.Contains(value, ",") {
		stringVals = strings.Split(value, ",")
	} else {
		vals, err := shellescape.Split(value)
		if err != nil {
			return fmt.Errorf("can't parse int slice value %q: %w", value, err)
		}
		stringVals = vals
	}
	for _, val := range stringVals {
		if val == "" {
			continue
		}
		unq, err := shellescape.Unquote(strings.TrimSpace(val))
		if err != nil {
			return fmt.Errorf("can't parse int slice value %q: %w", value, err)
		}
		intVal, err := strconv.Atoi(unq)
		if err != nil {
			return fmt.Errorf("can't parse int slice value %q: %w", value, err)
		}
		newVals = append(newVals, intVal)
	}
	for _, newVal := range newVals {
		var exists bool
		for _, oldVal := range oldVals {
			if oldVal == newVal {
				exists = true
			}
		}
		if !exists {
			oldVals = append(oldVals, newVal)
		}
	}

	v.Set(oldVals, originType, origin)
	return nil
}

// String returns the value as a string.
func (v *IntListValue) String() string {
	val, _ := v.Get()
	strSlice := make([]string, len(val))
	for i, v := range val {
		strSlice[i] = strconv.Itoa(v)
	}
	return formatStringSlice(strSlice, ',')
}

// PathValue is a configuration value that holds a path. It expands the path using the user's home directory and
// it also makes the path absolute if it is not already.
type PathValue struct {
	Value[string]
}

// SetString sets the value of the path and its origin.
func (v *PathValue) SetString(value string, originType ValueOriginType, origin string) error {
	// TODO - path expansion based on origin
	value, err := shellescape.Unquote(value)
	if err != nil {
		return fmt.Errorf("can't parse path value %q: %w", value, err)
	}

	if !filepath.IsAbs(value) {
		if origin != "" {
			value = filepath.Join(filepath.Dir(origin), value)
		} else {
			value = filepath.Join(home(), value)
		}
	}

	value, err = homedir.Expand(value)
	if err != nil {
		return fmt.Errorf("can't expand path value %q: %w", value, err)
	}

	if !filepath.IsAbs(value) {
		value = filepath.Join(home(), value)
	}

	v.Set(value, originType, origin)
	return nil
}

// String returns the value as a string.
func (v *PathValue) String() string {
	val, _ := v.Get()
	return shellescape.Quote(val)
}

// PathListValue a list of [PathValue] entries. Duplicates are skipped. If the existing list
// is set from the defaults, the list is cleared before setting the new value.
type PathListValue struct {
	StringListValue
}

// SetString appends the value to the slice and sets the origin of the value.
func (v *PathListValue) SetString(value string, originType ValueOriginType, origin string) error {
	var oldVals []string
	if originType == ValueOriginDefault || v.OriginType() != ValueOriginDefault {
		if val, ok := v.Get(); ok {
			oldVals = val
		}
	}

	paths, err := parseStringSliceValue(value)
	if err != nil {
		return fmt.Errorf("can't parse path slice value %q: %w", value, err)
	}

	for _, path := range paths {
		np := &PathValue{}
		if err := np.SetString(path, originType, origin); err != nil {
			return err
		}
		path, _ := np.Get()
		if !slices.Contains(oldVals, path) {
			oldVals = append(oldVals, path)
		}
	}
	v.Set(oldVals, originType, origin)
	return nil
}

// String returns the value as a string.
func (v *PathListValue) String() string {
	val, _ := v.Get()
	return formatStringSlice(val, ' ')
}

// AppendingStringListValue is like a [StringListValue] but it always appends the value to the existing value
// even if a value was already set.
type AppendingStringListValue struct {
	StringListValue
}

// SetString appends the value to the existing value.
func (v *AppendingStringListValue) SetString(value string, originType ValueOriginType, origin string) error {
	if value == "" {
		return nil
	}
	newVals, err := parseStringSliceValue(value)
	if err != nil {
		return fmt.Errorf("can't parse string slice value %q: %w", value, err)
	}
	v.value = append(v.value, newVals...)
	v.originType = originType
	v.origin = origin
	return nil
}

// ModifiableStringListValue is like [StringSliceValue] but the list can be prefixed with +, - or ^ to alter how
// the list is modified.
//
// + - appends the value to the existing list
// - - removes the given value from the existing list. the values can be wildcard patterns.
// ^ - clears the list and sets the value
//
// This is used in several fields in the ssh configuration, such as the lists of algorithms.
type ModifiableStringListValue struct {
	StringListValue
}

// SetString appends the value to the slice and sets the origin of the value or if a prefix is used,
// it modifies the list accordingly.
func (v *ModifiableStringListValue) SetString(value string, originType ValueOriginType, origin string) error {
	if value == "" {
		return nil
	}

	// Check for a prefix
	prefix := value[0]
	if prefix == '+' || prefix == '-' || prefix == '^' {
		value = value[1:]
	}

	valuesToSet, err := parseStringSliceValue(value)
	if err != nil {
		return fmt.Errorf("can't parse string slice value %q: %w", value, err)
	}

	var finalValues []string
	switch prefix {
	case '+':
		finalValues = v.appendUniqueValues(valuesToSet)
	case '-':
		finalValues = v.removeAllOccurrences(valuesToSet)
	case '^':
		finalValues = v.prependUniqueValues(valuesToSet)
	default:
		finalValues = valuesToSet
	}

	v.value = finalValues
	v.originType = originType
	v.origin = origin

	return nil
}

// parseStringSliceValue parses a comma-separated string into a slice.
func parseStringSliceValue(value string) ([]string, error) {
	if strings.Contains(value, ",") {
		newVals := strings.Split(value, ",")
		for i, val := range newVals {
			v, err := shellescape.Unquote(strings.TrimSpace(val))
			if err != nil {
				return nil, fmt.Errorf("can't parse string slice value %q: %w", value, err)
			}
			newVals[i] = v
		}
		return newVals, nil
	}
	v, err := shellescape.Split(value)
	if err != nil {
		return nil, fmt.Errorf("can't parse string slice value %q: %w", value, err)
	}
	return v, nil
}

// appendUniqueValues adds new values to the slice, avoiding duplicates.
func (v *ModifiableStringListValue) appendUniqueValues(newValues []string) []string {
	existingValues, _ := v.Get()
	for _, newVal := range newValues {
		if !slices.Contains(existingValues, newVal) {
			existingValues = append(existingValues, newVal)
		}
	}
	return existingValues
}

// removeAllOccurrences removes all occurrences of the specified values from the slice.
func (v *ModifiableStringListValue) removeAllOccurrences(valuesToRemove []string) []string {
	existingValues, _ := v.Get()
	for _, removeVal := range valuesToRemove {
		existingValues = filterOutMatchPattern(existingValues, removeVal)
	}
	return existingValues
}

// prependUniqueValues prepends new values to the slice after removing any existing occurrences.
func (v *ModifiableStringListValue) prependUniqueValues(newValues []string) []string {
	existingValues, _ := v.Get()
	existingValues = filterOutMultiple(existingValues, newValues)
	return append(newValues, existingValues...)
}

// filterOut removes a specific value from a slice.
func filterOut(slice []string, value string) []string {
	result := []string{}
	for _, v := range slice {
		if v != value {
			result = append(result, v)
		}
	}
	return result
}

// filterOutMatchPattern removes a specific value from a slice.
// The value can be a match pattern and remove multiple values.
func filterOutMatchPattern(slice []string, value string) []string {
	result := []string{}
	for _, v := range slice {
		if matches, err := matchesPattern(value, v); err != nil || !matches {
			result = append(result, v)
		}
	}
	return result
}

// filterOutMultiple removes multiple values from a slice.
func filterOutMultiple(slice []string, valuesToRemove []string) []string {
	for _, removeVal := range valuesToRemove {
		slice = filterOut(slice, removeVal)
	}
	return slice
}

// RemovableStringListValue is a configuration value that holds a slice of strings. It is used in the ssh configuration
// for the SendEnv field. Prefixing the value with - removes the value from the list. Like [AppendingStringListValue],
// without the prefix it always appends even if a value was set before.
type RemovableStringListValue struct {
	StringListValue
}

// SetString appends the value to the slice and sets the origin of the value or if a prefix is used,
// it modifies the list accordingly.
func (v *RemovableStringListValue) SetString(value string, originType ValueOriginType, origin string) error {
	if value == "" {
		return nil
	}
	vals, _ := v.Get()
	var remove bool
	if strings.HasPrefix(value, "-") {
		remove = true
		value = value[1:]
	}

	newVals, err := parseStringSliceValue(value)
	if err != nil {
		return fmt.Errorf("can't parse string slice value %q: %w", value, err)
	}
	if remove {
		vals = filterOutMultiple(vals, newVals)
	} else {
		vals = append(vals, newVals...)
	}
	v.value = vals
	v.originType = originType
	v.origin = origin
	return nil
}
