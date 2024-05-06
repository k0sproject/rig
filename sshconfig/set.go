package sshconfig

import (
	"context"
	"crypto/sha1" //nolint:gosec // sha1 is used for hasging certain filenames in ssh_config
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/k0sproject/rig/v2/log"
	"github.com/k0sproject/rig/v2/sh/shellescape"
	"github.com/k0sproject/rig/v2/sshconfig/options"
)

var (
	errFieldNotFound = errors.New("field not found")
	errInvalidField  = errors.New("invalid field")
	errInvalidValue  = errors.New("invalid value")
	errInvalidObject = errors.New("invalid object")

	errCanonicalizationFailed = errors.New("hostname canonicalization failed")

	// There's no need to rediscover the struct fields every time for the same type,
	// so they're cached. When the pre-parsed struct fields are found, scanning
	// for reflect.Value's for an instance's fields is faster too because the
	// struct field's Index can be used to call FieldByIndex.
	//
	// The expected use-case is that there will be one or many instances of the
	// single type, not many instances of different types.
	sfCache map[reflect.Type]map[string]reflect.StructField
	sfcMu   sync.Mutex
)

type parserPhase int

const (
	none = "none" // the string "none" is used in some fields to indicate no value
	yes  = "yes"  // the string "yes" is used as the boolean true value
	no   = "no"   // the string "no" is used as the boolean false value

	phaseRegular  parserPhase = iota // when setting regular values
	phaseDefaults                    // when setting values from defaults
	phaseFinal                       // when performing a finall pass
)

// Setter is a reflect based Setter for ssh configuration struct fields.
type Setter struct {
	wantFinal bool

	// The original host alias before any hostname canonicalization has been applied.
	OriginalHost string
	// When true, unknown fields will cause an error unless they match patterns defined in the IgnoreUnknown field.
	ErrorOnUnknownFields bool

	executor executor

	elem       reflect.Value
	elemFields map[string]reflect.Value

	home string

	phase parserPhase
}

// NewSetter creates a ssh config value setter for the given object. It can be used to set
// values on the object's fields using the same rules as the openssh client configuration
// parser via the Set(key, values...) function.
//
// It is used internally by the [Parser] but it can be used to set values manually.
func NewSetter(obj any) (*Setter, error) {
	elem, err := getElem(obj)
	if err != nil {
		return nil, err
	}
	setter := &Setter{elem: elem, executor: defaultExecutor{}, home: userhome()}
	setter.discoverFields()
	setter.initHost()
	return setter, nil
}

// sets the phase of the setter to "phaseDefaults" which is used when setting values from defaults.
// there are some fields that behave differently when setting defaults, so this is used to track that.
func (s *Setter) applyingDefaults(state bool) {
	if state {
		s.phase = phaseDefaults
	} else {
		s.phase = phaseFinal
	}
}

// sets the phase of the setter to "phaseFinal" which is used when performing a final pass.
// blocks like "Match final" are only applied in the final pass.
func (s *Setter) doingFinal() {
	s.phase = phaseFinal
}

func (s *Setter) initHost() {
	if s.OriginalHost != "" {
		return
	}
	s.OriginalHost = s.getHost()
}

func getElem(obj any) (reflect.Value, error) {
	objVal := reflect.ValueOf(obj)

	if objVal.Kind() != reflect.Ptr {
		return reflect.Value{}, fmt.Errorf("%w: object is not a pointer", errInvalidObject)
	}

	if objVal.IsNil() {
		return reflect.Value{}, fmt.Errorf("%w: object is nil", errInvalidObject)
	}

	structVal := objVal.Elem()
	if structVal.Kind() != reflect.Struct {
		return reflect.Value{}, fmt.Errorf("%w: object is not a struct", errInvalidObject)
	}
	return structVal, nil
}

func expectOne(v []string) error {
	if len(v) != 1 {
		return fmt.Errorf("%w: expected one value, got %d", errInvalidValue, len(v))
	}
	if v[0] == "" {
		return fmt.Errorf("%w: expected one value, got none", errInvalidValue)
	}
	return nil
}

func expectAtLeastOne(v []string) error {
	if len(v) == 0 || v[0] == "" {
		return fmt.Errorf("%w: expected at least one value, got none", errInvalidValue)
	}
	return nil
}

func (s *Setter) discoverFields() {
	sfcMu.Lock()
	defer sfcMu.Unlock()

	var sfields map[string]reflect.StructField
	if sfCache == nil {
		sfCache = make(map[reflect.Type]map[string]reflect.StructField)
	}

	sf, cached := sfCache[s.elem.Type()]
	if cached {
		sfields = sf
		s.elemFields = make(map[string]reflect.Value)
		for k, v := range sfields {
			s.elemFields[k] = s.elem.FieldByIndex(v.Index)
		}
		return
	}

	sfields = make(map[string]reflect.StructField)
	sfCache[s.elem.Type()] = sfields

	s.elemFields = make(map[string]reflect.Value)

	for i := range s.elem.NumField() {
		field := s.elem.Field(i)
		structField := s.elem.Type().Field(i)
		s.elemFields[structField.Name] = field
		sfields[structField.Name] = structField
	}
}

func (s *Setter) fieldByName(key string) (reflect.Value, bool) {
	if f, ok := s.elemFields[key]; ok {
		return f, ok
	}
	return reflect.Value{}, false
}

func (s *Setter) get(key string, expectedKinds ...reflect.Kind) (reflect.Value, error) { //nolint:cyclop
	field, ok := s.fieldByName(key)
	if !ok {
		return reflect.Value{}, fmt.Errorf("%w: field %q is not found", errFieldNotFound, key)
	}
	if !field.CanSet() {
		return reflect.Value{}, fmt.Errorf("%w: field %q is not settable", errInvalidObject, key)
	}

	isPtr := field.Kind() == reflect.Ptr
	expectsPtr := len(expectedKinds) > 0 && expectedKinds[0] == reflect.Ptr

	if isPtr && !expectsPtr {
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}
		field = field.Elem()
	} else if !isPtr && expectsPtr {
		return reflect.Value{}, fmt.Errorf("%w: expected a pointer for field %q", errInvalidField, key)
	}

	if !expectsPtr && len(expectedKinds) == 1 && field.Kind() != expectedKinds[0] {
		return reflect.Value{}, fmt.Errorf("%w: field %q is not of the expected type %s", errInvalidField, key, expectedKinds[0])
	} else if len(expectedKinds) == 2 {
		if expectedKinds[0] == reflect.Slice && (field.Kind() != reflect.Slice || field.Type().Elem().Kind() != expectedKinds[1]) {
			return reflect.Value{}, fmt.Errorf("%w: field %q is not a slice of the expected type", errInvalidField, key)
		}
	}

	return field, nil
}

func (s *Setter) setString(key string, values ...string) error {
	if err := expectOne(values); err != nil {
		return err
	}
	field, err := s.get(key, reflect.String)
	if err != nil {
		return err
	}
	if field.String() == "" {
		field.SetString(values[0])
	}
	return nil
}

func (s *Setter) setStringSlice(key string, values ...string) error {
	if err := expectAtLeastOne(values); err != nil {
		return err
	}
	field, err := s.get(key, reflect.Slice, reflect.String)
	if err != nil {
		return err
	}
	if field.Len() > 0 {
		return nil
	}
	field.Set(reflect.ValueOf(values))
	return nil
}

func (s *Setter) setStringSliceCSV(key string, values ...string) error {
	if err := expectOne(values); err != nil {
		return err
	}
	field, err := s.get(key, reflect.Slice, reflect.String)
	if err != nil {
		if f, err := s.get(key, reflect.String); err == nil {
			if f.String() == "" {
				f.SetString(values[0])
			}
			return nil
		}
		return err
	}
	if field.Len() > 0 {
		return nil
	}
	field.Set(reflect.ValueOf(strings.Split(values[0], ",")))
	return nil
}

func (s *Setter) setBool(key string, values ...string) error {
	if err := expectOne(values); err != nil {
		return err
	}
	bval, err := options.BooleanOption(values[0]).Normalize()
	if err != nil {
		return fmt.Errorf("%w: field %q: %w", errInvalidValue, key, err)
	}
	value := reflect.ValueOf(bval)
	field, err := s.get(key, value.Kind())
	if err != nil {
		if field, err := s.get(key, reflect.Ptr, reflect.Bool); err == nil {
			if !field.IsNil() {
				return nil
			}
			boolPtr := reflect.New(reflect.TypeOf(bval.IsTrue()))
			boolPtr.Elem().SetBool(bval.IsTrue())
			field.Set(boolPtr)
			return nil
		}
		return fmt.Errorf("%w: field %q is not a BooleanOption or a pointer to a bool", errInvalidField, key)
	}
	if field.String() != "" {
		return nil
	}
	field.Set(value)
	return nil
}

func (s *Setter) setInt(key string, values ...string) error {
	if err := expectOne(values); err != nil {
		return err
	}
	var ival int64
	if strings.HasPrefix(values[0], "0") && len(values[0]) > 1 {
		v, err := strconv.ParseInt(values[0], 8, 64)
		if err != nil {
			return fmt.Errorf("%w: invalid octal int value %q", errInvalidValue, values[0])
		}
		ival = v
	} else {
		v, err := strconv.ParseInt(values[0], 10, 64)
		if err != nil {
			return fmt.Errorf("%w: invalid int value %q", errInvalidValue, values[0])
		}
		ival = v
	}
	field, err := s.get(key, reflect.Int)
	if err != nil {
		return err
	}
	if field.Kind() == reflect.Ptr {
		if !field.IsNil() {
			return nil
		}
		intPtr := reflect.New(reflect.TypeOf(ival))
		intPtr.Elem().SetInt(ival)
		field.Set(intPtr)
	} else {
		if field.Int() != 0 {
			return nil
		}
		field.SetInt(ival)
	}
	return nil
}

func (s *Setter) setUint(key string, values ...string) error {
	if err := expectOne(values); err != nil {
		return err
	}
	var uval uint64
	if strings.HasPrefix(values[0], "0") && len(values[0]) > 1 { //nolint:nestif
		v, err := strconv.ParseUint(values[0], 8, 64)
		if err != nil {
			return fmt.Errorf("%w: invalid octal uint value %q", errInvalidValue, values[0])
		}
		if v > uint64(math.MaxUint) {
			return fmt.Errorf("%w: invalid uint value %q", errInvalidValue, values[0])
		}
		uval = v
	} else {
		v, err := strconv.ParseUint(values[0], 10, 64)
		if err != nil {
			return fmt.Errorf("%w: invalid uint value %q", errInvalidValue, values[0])
		}
		if v > uint64(math.MaxUint) {
			return fmt.Errorf("%w: invalid uint value %q", errInvalidValue, values[0])
		}
		uval = v
	}
	field, err := s.get(key, reflect.Uint)
	if err != nil {
		return err
	}
	if field.Kind() == reflect.Ptr {
		if !field.IsNil() {
			return nil
		}
		uintPtr := reflect.New(reflect.TypeOf(uval))
		uintPtr.Elem().SetUint(uval)
		field.Set(uintPtr)
		return nil
	}
	if field.Uint() != 0 {
		return nil
	}
	field.SetUint(uval)
	return nil
}

// setDuration sets a duration field value. If the value is none, the duration is set to 0. If a unit is not
// specified, "s" is assumed. The field can be a duration or a pointer to a duration. Note that if the field is not
// a pointer, a zero value is considered unset.
func (s *Setter) setDuration(key string, values ...string) error { //nolint:cyclop
	if err := expectOne(values); err != nil {
		return err
	}

	value := values[0]

	var dur time.Duration

	if value != none {
		if value[len(value)-1] >= '0' && value[len(value)-1] <= '9' {
			value += "s"
		}
		var err error
		dur, err = time.ParseDuration(value)
		if err != nil {
			return fmt.Errorf("%w: invalid duration value %q: %w", errInvalidValue, value, err)
		}
	}

	field, err := s.get(key, reflect.Ptr, reflect.Int64)
	if err != nil {
		if f, err := s.get(key, reflect.Int64); err == nil {
			field = f
		} else {
			return fmt.Errorf("%w: field must be a duration or a pointer to a duration", err)
		}
	} else if !field.IsNil() {
		return nil
	}

	durationType := reflect.TypeOf(time.Duration(0))
	switch {
	case field.Kind() == reflect.Ptr:
		if field.Type().Elem() != durationType {
			return fmt.Errorf("%w: field must be a pointer to a duration", errInvalidField)
		}
		if !field.IsNil() {
			return nil
		}
	case field.Type() != durationType:
		return fmt.Errorf("%w: field must be a duration", errInvalidField)
	case field.Int() != 0:
		return nil
	}

	if field.Kind() == reflect.Ptr {
		durPtr := reflect.New(durationType)
		durPtr.Elem().Set(reflect.ValueOf(dur))
		field.Set(durPtr)
	} else {
		field.Set(reflect.ValueOf(dur))
	}

	return nil
}

func normalizePath(value string) string {
	if runtime.GOOS == "windows" {
		// this is a hack to support the __PROGRAMDATA__ in the ssh config dumps on windows.
		value = strings.ReplaceAll(value, "__PROGRAMDATA__", os.Getenv("PROGRAMDATA"))
	}

	return strings.ReplaceAll(filepath.Clean(value), "\\", "/")
}

func (s *Setter) setPath(key string, values ...string) error {
	if err := expectOne(values); err != nil {
		return err
	}
	return s.setString(key, normalizePath(values[0]))
}

func (s *Setter) setPathList(key string, values ...string) error {
	if err := expectAtLeastOne(values); err != nil {
		return err
	}

	newValues := make([]string, len(values))
	var j int
	for _, value := range values {
		if value == "" {
			continue
		}
		newValues[j] = normalizePath(value)
		j++
	}
	return s.setStringSlice(key, newValues[:j]...)
}

// appendPathList appends a path list field value. The value is expected to be a path or a list of paths.
// Unlike setPathList, this function appends the values to the existing list except when performing
// parsing of the defaults.
func (s *Setter) appendPathList(key string, values ...string) error {
	if err := expectAtLeastOne(values); err != nil {
		return err
	}

	field, err := s.get(key, reflect.Slice, reflect.String)
	if err != nil {
		return err
	}
	if slices.Contains(values, none) {
		if len(values) > 1 {
			return fmt.Errorf("%w: none cannot be used with other values", errInvalidValue)
		}
		field.Set(reflect.ValueOf(values))
	}

	oldValues, ok := field.Interface().([]string)
	if !ok {
		return fmt.Errorf("%w: field %q is not a slice of strings", errInvalidField, key)
	}
	if len(oldValues) == 1 && oldValues[0] == none {
		return nil
	}
	if len(oldValues) > 0 && s.phase == phaseDefaults {
		// defaults should not be appended to a populated list
		return nil
	}
	for _, value := range values {
		value = normalizePath(value)
		if !slices.Contains(oldValues, value) {
			oldValues = append(oldValues, value)
		}
	}
	field.Set(reflect.ValueOf(oldValues))
	return nil
}

// appendStringList appends strings to a string list field value. If the list is already populated, values
// from defaults will not be appended.
func (s *Setter) appendStringList(key string, values ...string) error {
	if err := expectAtLeastOne(values); err != nil {
		return err
	}
	field, err := s.get(key, reflect.Slice, reflect.String)
	if err != nil {
		return err
	}
	if slices.Contains(values, none) {
		if len(values) > 1 {
			return fmt.Errorf("%w: none cannot be used with other values", errInvalidValue)
		}
		field.Set(reflect.ValueOf(values))
		return nil
	}
	oldValues, ok := field.Interface().([]string)
	if !ok {
		return fmt.Errorf("%w: field %q is not a slice of strings", errInvalidField, key)
	}
	if len(oldValues) == 1 && oldValues[0] == none {
		return nil
	}
	if len(oldValues) > 0 && s.phase == phaseDefaults {
		// defaults should not be appended to a populated list
		return nil
	}
	for _, value := range values {
		if !slices.Contains(oldValues, value) {
			oldValues = append(oldValues, value)
		}
	}
	field.Set(reflect.ValueOf(oldValues))
	return nil
}

func (s *Setter) setRekeyLimitOption(key string, values ...string) error { //nolint:cyclop
	if len(values) < 1 || len(values) > 2 {
		return fmt.Errorf("%w: expected 1 or 2 values, got %d", errInvalidValue, len(values))
	}
	byteVal := values[0]
	var suffix byte
	if byteVal[len(byteVal)-1] < '0' || byteVal[len(byteVal)-1] > '9' {
		suffix = byteVal[len(byteVal)-1]
		byteVal = byteVal[:len(byteVal)-1]
	}
	byteInt64, err := strconv.ParseInt(byteVal, 10, 64)
	if err != nil {
		return fmt.Errorf("%w: failed to parse byte value: %w", errInvalidValue, err)
	}
	switch suffix {
	case 'k', 'K':
		byteInt64 *= 1024
	case 'm', 'M':
		byteInt64 *= 1024 * 1024
	case 'g', 'G':
		byteInt64 *= 1024 * 1024 * 1024
	}

	var maxTime time.Duration
	if len(values) == 2 {
		timeVal := options.AddIntervalSuffix(values[1])
		d, err := time.ParseDuration(timeVal)
		if err != nil {
			return fmt.Errorf("%w: failed to parse time value: %w", errInvalidValue, err)
		}
		maxTime = d
	}
	opt := options.RekeyLimitOption{MaxData: byteInt64, MaxTime: maxTime}
	value := reflect.ValueOf(opt)
	field, err := s.get(key, value.Kind())
	if err != nil {
		if f, err := s.get(key, reflect.Slice, reflect.String); err == nil {
			if f.Len() > 0 {
				return nil
			}
			f.Set(reflect.ValueOf(values))
			return nil
		}
		return fmt.Errorf("%w: field %q is not a RekeyLimitOption or a string slice", errInvalidField, key)
	}
	rk, ok := field.Interface().(options.RekeyLimitOption)
	if !ok {
		return fmt.Errorf("%w: field %q is not a RekeyLimitOption", errInvalidField, key)
	}
	if rk.String() != "" {
		return nil
	}
	field.Set(value)
	return nil
}

func (s *Setter) setChannelTimeoutOption(key string, values ...string) error {
	if err := expectAtLeastOne(values); err != nil {
		return err
	}
	res := make(map[string]time.Duration)
	for _, value := range values {
		parts := strings.Split(value, "=")
		if len(parts) != 2 {
			return fmt.Errorf("%w: invalid channel timeout value %q", errInvalidValue, value)
		}

		switch parts[0] {
		case "agent-connection", "direct-tcpip", "direct-streamlocal@openssh.com", "forwarded-tcpip", "forwarded-streamlocal@openssh.com", "session", "tun-connection", "x11-connection":
		// valid
		default:
			return fmt.Errorf("%w: invalid channel timeout type %q", errInvalidValue, parts[0])
		}
		timeVal := options.AddIntervalSuffix(parts[1])
		d, err := time.ParseDuration(timeVal)
		if err != nil {
			return fmt.Errorf("%w: failed to parse time value %q: %w", errInvalidValue, parts[1], err)
		}
		res[parts[0]] = d
	}
	value := reflect.ValueOf(res)
	field, err := s.get(key, value.Kind())
	if err != nil {
		return err
	}
	if field.Len() > 0 {
		return nil
	}
	field.Set(value)
	return nil
}

func (s *Setter) setAddKeysToAgentOption(key string, values ...string) error {
	if len(values) < 1 || len(values) > 2 {
		return fmt.Errorf("%w: expected 1 or 2 values, got %d", errInvalidValue, len(values))
	}

	normalized, err := options.AddKeysToAgentOption(strings.Join(values, " ")).Normalize()
	if err != nil {
		return fmt.Errorf("not a valid %q value %q: %w", key, values, err)
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

func (s *Setter) setIPQoSOption(key string, values ...string) error {
	if len(values) < 1 || len(values) > 2 {
		return fmt.Errorf("%w: expected 1 or 2 values, got %d", errInvalidValue, len(values))
	}
	res := make([]string, len(values))
	for i, v := range values {
		nv, err := options.IPQoSOption(v).Normalize()
		if err != nil {
			return fmt.Errorf("not a valid %q value: %w", key, err)
		}
		res[i] = nv.String()
	}
	value := reflect.ValueOf(res)
	field, err := s.get(key, value.Kind())
	if err != nil {
		if f, err := s.get(key, reflect.Slice, reflect.String); err == nil {
			if f.Len() > 0 {
				return nil
			}
			f.Set(reflect.ValueOf(values))
			return nil
		}
		return fmt.Errorf("%w: field %q is not a string slice or a []IPQoSOption", errInvalidField, key)
	}
	if field.Len() > 0 {
		return nil
	}
	field.Set(value)
	return nil
}

func (s *Setter) setForwardOption(key string, values ...string) error {
	if len(values) != 2 {
		return fmt.Errorf("%w: expected 2 values, got %d", errInvalidValue, len(values))
	}
	var res map[string]string
	value := reflect.ValueOf(res)
	field, err := s.get(key, value.Kind())
	if err != nil {
		return err
	}
	if field.IsNil() {
		res = make(map[string]string)
	} else {
		var ok bool
		res, ok = field.Interface().(map[string]string)
		if !ok {
			return fmt.Errorf("%w: field %q is not a map[string]string", errInvalidField, key)
		}
	}
	res[values[0]] = values[1]
	field.Set(reflect.ValueOf(res))
	return nil
}

func (s *Setter) setPort(key string, values ...string) error {
	if err := expectOne(values); err != nil {
		return err
	}
	port, err := strconv.Atoi(values[0])
	if err != nil {
		return fmt.Errorf("%w: invalid port value %q", errInvalidValue, values[0])
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("%w: invalid port value %q", errInvalidValue, values[0])
	}
	return s.setInt(key, values[0])
}

// setSendEnvOption sets the sendenv field value. The field is "always appending" but you can also
// use the - prefix to remove a value.
func (s *Setter) setSendEnvOption(key string, values ...string) error {
	if err := expectAtLeastOne(values); err != nil {
		return err
	}
	field, err := s.get(key, reflect.Slice, reflect.String)
	if err != nil {
		return err
	}
	for _, value := range values {
		if value == "" {
			continue
		}
		if value[0] == '-' {
			value = value[1:]
			oldVals, ok := field.Interface().([]string)
			if !ok {
				return fmt.Errorf("%w: field %q is not a slice of strings", errInvalidField, key)
			}
			var newVals []string
			for _, v := range oldVals {
				match, err := patternMatch(v, value)
				if err != nil {
					return fmt.Errorf("%w: failed to match %q with %q: %w", errInvalidValue, value, v, err)
				}
				if match {
					continue
				}
				newVals = append(newVals, v)
			}
			field.Set(reflect.ValueOf(newVals))
		} else {
			field.Set(reflect.Append(field, reflect.ValueOf(value)))
		}
	}
	return nil
}

// setSetEnvOption sets the setenv field value. The field is "always appending".
func (s *Setter) setSetEnvOption(key string, values ...string) error {
	if err := expectAtLeastOne(values); err != nil {
		return err
	}
	var res map[string]string
	value := reflect.ValueOf(res)
	field, err := s.get(key, value.Kind())
	if err != nil {
		return err
	}
	if field.IsNil() {
		res = make(map[string]string)
	} else {
		r, ok := field.Interface().(map[string]string)
		if !ok {
			return fmt.Errorf("%w: field %q is not a map of strings", errInvalidField, key)
		}
		if len(r) > 0 {
			return nil
		}
		res = r
	}
	for _, value := range values {
		if value == "" {
			continue
		}
		parts := strings.SplitN(value, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("%w: invalid setenv value %q", errInvalidValue, value)
		}
		if parts[0] == "" {
			return fmt.Errorf("%w: invalid setenv value %q", errInvalidValue, value)
		}
		res[parts[0]] = parts[1]
	}
	field.Set(reflect.ValueOf(res))
	return nil
}

// setColonSeparatedValues sets a string map field value from a list of key:value pairs.
// It supports the none keyword.
func (s *Setter) setColonSeparatedValues(key string, values ...string) error {
	if err := expectAtLeastOne(values); err != nil {
		return err
	}
	field, err := s.get(key, reflect.Slice, reflect.String)
	if err != nil {
		return err
	}
	if field.Len() > 0 {
		return nil
	}
	if slices.Contains(values, none) {
		if len(values) > 1 {
			return fmt.Errorf("%w: 'none' must be the only value", errInvalidValue)
		}
		field.Set(reflect.ValueOf(values))
		return nil
	}
	if strings.ToLower(key) == "permitremoteopen" && slices.Contains(values, "any") {
		if len(values) > 1 {
			return fmt.Errorf("%w: 'any' must be the only value", errInvalidValue)
		}
		field.Set(reflect.ValueOf(values))
		return nil
	}

	for _, value := range values {
		if strings.Count(value, ":") != 1 {
			return fmt.Errorf("%w: invalid %q value %q", errInvalidValue, key, value)
		}
	}
	field.Set(reflect.ValueOf(values))
	return nil
}

func (s *Setter) getHost() string {
	field, err := s.get("Host", reflect.String)
	if err != nil {
		return ""
	}
	return field.String()
}

func (s *Setter) setHost(_ string, values ...string) error {
	if err := expectOne(values); err != nil {
		return err
	}
	field, err := s.get("Host", reflect.String)
	if err != nil {
		return err
	}
	if s.OriginalHost == values[0] {
		return nil
	}
	if s.OriginalHost == "" {
		s.OriginalHost = values[0]
	}
	field.SetString(values[0])
	return nil
}

// HostChanged returns true if the value of "Host" (the host alias initially used to connect) has been changed.
// This happens if hostname canonicalization is performed. When connecting to the target, you should first
// check if Hostname is defined, if not, use the Host value.
func (s *Setter) HostChanged() bool {
	field, err := s.get("Host", reflect.String)
	if err != nil {
		return false
	}
	return field.String() != s.OriginalHost
}

func (s *Setter) setEscapeCharOption(key string, values ...string) error {
	if err := expectOne(values); err != nil {
		return err
	}
	escOpt, err := options.EscapeCharOption(values[0]).Normalize()
	if err != nil {
		return fmt.Errorf("not a valid %q value: %w", key, err)
	}
	value := reflect.ValueOf(escOpt)
	field, err := s.get(key, value.Kind())
	if err != nil { //nolint:nestif
		if f, err := s.get(key, reflect.String); err == nil {
			if f.String() != "" {
				f.SetString(escOpt.String())
			}
			return nil
		}
		if f, err := s.get(key, reflect.Int); err == nil {
			if f.Int() != 0 {
				f.SetInt(int64(escOpt.Byte()))
			}
			return nil
		}
		if f, err := s.get(key, reflect.Uint8); err == nil {
			if f.Uint() != 0 {
				f.SetUint(uint64(escOpt.Byte()))
			}
			return nil
		}
		return err
	}
	if field.String() != "" {
		return nil
	}
	field.Set(value)
	return nil
}

// Ignore a key.
func (s *Setter) ignore(key string, _ ...string) error {
	log.Trace(context.Background(), "ignoring key", "key", key)
	return nil
}

// Set a value for a field by key name using the precedence rules of the SSH configuration file syntax.
//
// Note that some of the fields take their values as separate strings and some as a comma-separated list, most
// only accept a single value.
func (s *Setter) Set(key string, values ...string) error {
	key = strings.ToLower(key)
	info, ok := knownKeys[key]

	var err error
	switch {
	case !ok:
		err = fmt.Errorf("%w: unknown key %q", errFieldNotFound, key)
	case info.setFunc == nil:
		return fmt.Errorf("%w: field %q is not settable", errInvalidObject, key)
	default:
		err = info.setFunc(s, info.key, values...)
	}

	if errors.Is(err, errFieldNotFound) || errors.Is(err, errInvalidField) {
		if !s.ErrorOnUnknownFields {
			return nil
		}
		if !s.isInIgnoreUnknown(key) {
			return err
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to set %q: %w", info.key, err)
	}
	return nil
}

// Reset sets a field to its zero value. This is useful in testing.
func (s *Setter) Reset(key string) error {
	info, ok := knownKeys[strings.ToLower(key)]
	if !ok {
		return fmt.Errorf("%w: unknown key %q", errFieldNotFound, key)
	}
	field, ok := s.fieldByName(info.key)
	if !ok {
		return fmt.Errorf("%w: field %q is not found", errFieldNotFound, key)
	}
	// Set a pointer to nil and a non-pointer to zero value.
	if field.Kind() == reflect.Ptr {
		field.Set(reflect.New(field.Type().Elem()))
	} else {
		field.Set(reflect.Zero(field.Type()))
	}
	return nil
}

// Finalize goes through all the string slice fields and sets them to nil if they contain a single none value.
// It expands any tokens, environment variables or tilde-prefixed paths in the values for the keys where expansion
// is supported.
func (s *Setter) Finalize() error { //nolint:cyclop
	for _, info := range knownKeys {
		field, ok := s.fieldByName(info.key)
		if !ok {
			continue
		}
		switch field.Kind() { //nolint:exhaustive
		case reflect.String:
			newVal, err := s.ExpandString(info.key)
			if err != nil {
				return fmt.Errorf("%w: failed to expand %q: %w", errInvalidValue, info.key, err)
			}
			field.SetString(newVal)
		case reflect.Slice:
			if field.Len() == 1 && field.Index(0).String() == none {
				field.Set(reflect.Zero(field.Type()))
			}
			if field.Type().Elem().Kind() == reflect.String {
				newVal, err := s.ExpandSlice(info.key)
				if err != nil {
					return fmt.Errorf("%w: failed to expand %q: %w", errInvalidValue, info.key, err)
				}
				field.Set(reflect.ValueOf(newVal))
			}
		case reflect.Map:
			// do nothing unless it's a map[string]string
			if field.Type().Key().Kind() != reflect.String && field.Type().Elem().Kind() != reflect.String {
				continue
			}
			if info.key != "LocalForward" && info.key != "RemoteForward" {
				for _, k := range field.MapKeys() {
					value := field.MapIndex(k)
					if value.Len() == 1 && strings.Contains(value.Index(0).String(), "$") {
						newValue, err := shellescape.Expand(value.Index(0).String(), shellescape.ExpandNoDollarVars(), shellescape.ExpandErrorIfUnset())
						if err != nil {
							return fmt.Errorf("%w: failed to expand localforward value %q: %w", errInvalidValue, info.key, err)
						}
						field.SetMapIndex(k, reflect.ValueOf([]string{newValue}))
					}
				}
				continue
			}
		}
	}
	return nil
}

var tokenRe = regexp.MustCompile(`%[a-zA-Z%]`)

/*
%%
A literal ‘%’.
%C
Hash of %l%h%p%r%j.
%d
Local user's home directory.
%f
The fingerprint of the server's host key.
%H
The known_hosts hostname or address that is being searched for.
%h
The remote hostname.
%I
A string describing the reason for a KnownHostsCommand execution: either ADDRESS when looking up a host by address (only when CheckHostIP is enabled), HOSTNAME when searching by hostname, or ORDER when preparing the host key algorithm preference list to use for the destination host.
%i
The local user ID.
%j
The contents of the ProxyJump option, or the empty string if this option is unset.
%K
The base64 encoded host key.
%k
The host key alias if specified, otherwise the original remote hostname given on the command line.
%L
The local hostname.
%l
The local hostname, including the domain name.
%n
The original remote hostname, as given on the command line.
%p
The remote port.
%r
The remote username.
%T
The local tun(4) or tap(4) network interface assigned if tunnel forwarding was requested, or none otherwise.
%t
The type of the server host key, e.g. ssh-ed25519.
%u
The local username.
*/

func sshConnectionHash(str string) string {
	h := sha1.New() //nolint:gosec // sha1 is used for compatibility with OpenSSH

	// Write data to hash
	_, _ = io.WriteString(h, str)

	// Calculate final hash
	sum := h.Sum(nil)

	// Convert to hexadecimal string
	return hex.EncodeToString(sum)
}

func (s *Setter) expandToken(token string) (string, error) { //nolint:cyclop
	switch token {
	case "%%":
		return "%", nil
	case "%u":
		return username(), nil
	case "%d":
		return s.home, nil
	case "%h":
		if f, err := s.get("hostname", reflect.String); err == nil {
			if f.Len() > 0 {
				return f.String(), nil
			}
		}
		if f, err := s.get("Host", reflect.String); err == nil {
			if f.Len() > 0 {
				return f.String(), nil
			}
		}
		return "", fmt.Errorf("%w: no hostname found for token %%h expansion", errFieldNotFound)
	case "%p":
		if f, err := s.get("Port", reflect.Int); err == nil {
			if f.Int() > 0 {
				return strconv.Itoa(int(f.Int())), nil
			}
		}

		if f, err := s.get("Port", reflect.Uint); err == nil {
			if f.Uint() > 0 {
				return strconv.Itoa(int(f.Uint())), nil
			}
		}
		if f, err := s.get("Port", reflect.String); err == nil {
			if f.Len() > 0 {
				return f.String(), nil
			}
		}
		return "", fmt.Errorf("%w: no port found for token %%p expansion", errFieldNotFound)
	case "%n":
		return s.OriginalHost, nil
	case "%r":
		if f, err := s.get("User", reflect.String); err == nil {
			if f.Len() > 0 {
				return f.String(), nil
			}
		}
		return username(), nil
	case "%H":
		for _, field := range []string{"HostkeyAlias", "Hostname", "Host"} {
			if f, err := s.get(field, reflect.String); err == nil && f.Len() > 0 {
				return f.String(), nil
			}
		}
		return "", fmt.Errorf("%w: no hostname available for token %%H expansion", errFieldNotFound)
	case "%j":
		if f, err := s.get("ProxyJump", reflect.String); err == nil {
			if f.Len() > 0 {
				return f.String(), nil
			}
		}
		if f, err := s.get("ProxyJump", reflect.Slice, reflect.String); err == nil {
			if f.Len() > 0 {
				if slice, ok := f.Interface().([]string); ok {
					return strings.Join(slice, ","), nil
				}
				return "", fmt.Errorf("%w: failed to convert ProxyJump to string slice", errInvalidValue)
			}
		}
		return "", nil
	case "%L":
		h, err := os.Hostname()
		if err != nil {
			return "", fmt.Errorf("%w: failed to get local hostname for token %%L expansion: %w", errFieldNotFound, err)
		}
		return h, nil
	case "%C":
		toExpand, err := s.expand("%l%h%p%r%j", keyInfo{tokens: []string{"%l", "%h", "%p", "%r", "%j"}})
		if err != nil {
			return "", err
		}
		return sshConnectionHash(toExpand), nil
	}
	return "", fmt.Errorf("%w: unsupported token %q", errInvalidValue, token)
}

func (s *Setter) expand(value string, info keyInfo) (string, error) { //nolint:cyclop
	if value == "" {
		return "", nil
	}
	if len(info.tokens) == 0 && !info.tilde && !info.env {
		return value, nil
	}

	if info.env {
		nv, err := shellescape.Expand(value, shellescape.ExpandNoDollarVars(), shellescape.ExpandErrorIfUnset())
		if err != nil {
			return "", fmt.Errorf("%w: failed to expand environment variables in %q: %w", errInvalidValue, value, err)
		}
		value = nv
	}
	if info.tilde {
		if strings.HasPrefix(value, "~/") {
			value = s.home + value[1:]
			if value[1] == ':' {
				value = strings.ReplaceAll(value, "\\", "/")
			}
		}
	}
	var reErr error
	if len(info.tokens) > 0 {
		value = tokenRe.ReplaceAllStringFunc(value, func(token string) string {
			if slices.Contains(info.tokens, token) {
				replacement, err := s.expandToken(token)
				if err != nil {
					reErr = err
					return token
				}
				return replacement
			}
			reErr = fmt.Errorf("%w: unsupported token %q in %q", errInvalidValue, token, value)
			return token
		})
	}
	if reErr != nil {
		return "", reErr
	}
	return value, nil
}

// ExpandString expands any environment variables, tokens or tilde paths in the value.
// This is done automatically during the finalization phase, which can be disabled
// using the parser options.
func (s *Setter) ExpandString(key string) (string, error) {
	info, ok := knownKeys[strings.ToLower(key)]
	if !ok {
		return "", fmt.Errorf("%w: unknown key %q", errFieldNotFound, key)
	}
	field, ok := s.fieldByName(info.key)
	if !ok {
		return "", fmt.Errorf("%w: field %q is not found", errFieldNotFound, key)
	}
	if field.Kind() == reflect.Ptr {
		if field.IsNil() {
			return "", fmt.Errorf("%w: field %q is nil", errInvalidField, key)
		}
		field = field.Elem()
	}
	var str string
	switch field.Kind() { //nolint:exhaustive
	case reflect.String:
		str = field.String()
	case reflect.Slice:
		return "", fmt.Errorf("%w: field %q is a slice", errInvalidField, key)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Bool:
		return fmt.Sprint(field.Interface()), nil
	case reflect.Struct:
		var stringer fmt.Stringer
		var ok bool
		if field.CanInterface() {
			stringer, ok = field.Interface().(fmt.Stringer)
		} else if field.CanAddr() {
			stringer, ok = field.Addr().Interface().(fmt.Stringer)
		}
		if !ok {
			return "", fmt.Errorf("%w: field %q is not a stringer", errInvalidField, key)
		}
		str = stringer.String()
	}
	return s.expand(str, info)
}

// ExpandSlice expands any environment variables, tokens or tilde paths in the values.
// This is done automatically during the finalization phase, which can be disabled
// using the parser options.
func (s *Setter) ExpandSlice(key string) ([]string, error) { //nolint:cyclop
	info, ok := knownKeys[strings.ToLower(key)]
	if !ok {
		return nil, fmt.Errorf("%w: unknown key %q", errFieldNotFound, key)
	}
	field, ok := s.fieldByName(info.key)
	if !ok {
		return nil, fmt.Errorf("%w: field %q is not found", errFieldNotFound, key)
	}
	if field.Kind() != reflect.Slice {
		return nil, fmt.Errorf("%w: field %q is not a slice", errInvalidField, key)
	}
	if field.Len() == 0 {
		return nil, nil
	}
	var values []string
	for i := range field.Len() {
		f := field.Index(i)
		switch {
		case f.Kind() == reflect.String:
			values = append(values, f.String())
		case f.CanInterface():
			if str, ok := f.Interface().(fmt.Stringer); ok {
				values = append(values, str.String())
			}
		case f.CanAddr():
			if str, ok := f.Addr().Interface().(fmt.Stringer); ok {
				values = append(values, str.String())
			}
		default:
			return nil, fmt.Errorf("%w: field %q is not a stringer", errInvalidField, key)
		}
	}
	for i, value := range values {
		nv, err := s.expand(value, info)
		if err != nil {
			return nil, err
		}
		values[i] = nv
	}
	return values, nil
}

func (s *Setter) isInIgnoreUnknown(key string) bool {
	field, err := s.get("IgnoreUnknown", reflect.Slice, reflect.String)
	if err != nil {
		return false
	}
	for i := range field.Len() {
		match, err := patternMatch(key, field.Index(i).String())
		if err != nil {
			return false
		}
		if match {
			return true
		}
	}
	return false
}

// matchesHost checks if the Host directive matching conditions are met.
func (s *Setter) matchesHost(conditions ...string) (bool, error) {
	host, err := s.get("Host", reflect.String)
	if err != nil {
		return false, err
	}
	if host.Len() == 0 {
		return false, fmt.Errorf("%w: host field is empty", errInvalidObject)
	}
	match, err := patternMatchAll(host.String(), conditions...)
	if err != nil {
		return false, err
	}
	return match, nil
}

// matchesMatch checks if the Match directive conditions are met.
func (s *Setter) matchesMatch(conditions ...string) (bool, error) { //nolint:funlen,cyclop // TODO extract functions
	log.Trace(context.Background(), "matching Match directive", "conditions", conditions)
	for i := range len(conditions) {
		condition := conditions[i]
		log.Trace(context.Background(), "matching Match directive", "condition", condition)
		var negate bool
		if condition[0] == '!' {
			negate = true
			condition = condition[1:]
		}
		switch condition {
		case "all":
			// For 'all', return true. Using '!all' is useful maybe only for
			// commenting out a block, but it's a valid condition which is always false.
			return !negate, nil
		case "canonical", "final":
			// We're going to perform canonical and final during the same pass, so
			// we can treat them as synonyms.
			if s.phase == phaseFinal {
				if negate {
					// it's the final round but !final is used, so return false
					return false, nil
				}
				// Proceed to check other conditions if any
				continue
			}
			// Not on the final round
			if negate {
				// !canonical or !final is used, so proceed to check other conditions
				continue
			}
			s.wantFinal = true
			// Not on final round, 'canonical' or 'final' is used, so return false
			return false, nil
		}

		parts := strings.Split(condition, "=")
		if len(parts) != 2 {
			return false, fmt.Errorf("%w: invalid Match condition: %q", ErrSyntax, condition)
		}
		condition, quotedArgs := parts[0], parts[1]
		args, err := shellescape.Unquote(quotedArgs)
		if err != nil {
			return false, fmt.Errorf("%w: failed to unquote match condition %q: %w", ErrSyntax, args, err)
		}

		// Deal with "exec" condition because its parameter is parsed differently.
		if condition == "exec" { //nolint:nestif // TODO extract function
			cmdStr, err := s.expand(args, keyInfo{
				key:    "exec",
				tokens: tokenset1,
			})
			if err != nil {
				return false, fmt.Errorf("%w: failed to expand %q for Match exec condition: %w", ErrSyntax, args, err)
			}
			unq, err := shellescape.Split(cmdStr)
			if err != nil {
				return false, fmt.Errorf("%w: failed to process %q: %w", ErrSyntax, args, err)
			}
			cmd := unq[0]
			if len(unq) > 1 {
				unq = unq[1:]
			} else {
				unq = nil
			}
			log.Trace(context.Background(), "executing command from match directive", "condition", condition, "cmd", cmd, "args", unq)
			if s.executor.Run(cmd, unq...) != nil {
				log.Trace(context.Background(), "command failed", "condition", condition, "cmd", cmd, "args", unq, "error", err)
				if negate {
					return false, nil
				}
				continue
			}
			log.Trace(context.Background(), "command succeeded", "condition", condition, "cmd", cmd, "args", unq)
			if negate {
				return false, nil
			}
			continue
		}

		// The rest of the conditions take a comma separated list of arguments.
		argsSlice := strings.Split(args, ",")

		// Deal with "localnetwork" condition separately.
		if condition == "localnetwork" { //nolint:nestif
			match, err := matchLocalNetwork(argsSlice)
			if err != nil {
				return false, fmt.Errorf("failed to match local network: %w", err)
			}
			if match {
				if negate {
					return false, nil
				}
				continue
			}
			if negate {
				continue
			}
			return false, nil
		}

		// The rest of the conditions share an equal logic:
		// The args are patterns and depending on the condition, a different
		// value is matched against the patterns.
		//
		// The patterns are already in the args and the switch statement below
		// is used to determine the matchTarget.
		var matchTarget string

		// continue with the condition
		switch condition {
		case "user":
			// consume next arg
			user, err := s.get("User", reflect.String)
			if err != nil {
				return false, fmt.Errorf("%w: user field not found", ErrInvalidObject)
			}
			matchTarget = user.String()
		case "originalhost":
			matchTarget = s.OriginalHost
		case "localuser":
			matchTarget = username()
		case "host":
			if hostname, err := s.get("Hostname", reflect.String); err == nil && hostname.Len() > 0 {
				matchTarget = hostname.String()
			} else if host, err := s.get("Host", reflect.String); err == nil && host.Len() > 0 {
				matchTarget = host.String()
			} else {
				return false, fmt.Errorf("%w: host or hostname fields not found or empty", ErrInvalidObject)
			}
		case "tagged":
			tag, err := s.get("Tag", reflect.String)
			if err != nil {
				return false, fmt.Errorf("%w: tag field not found", ErrInvalidObject)
			}
			matchTarget = tag.String()
		default:
			return false, fmt.Errorf("%w: unknown match condition: %q", ErrSyntax, condition)
		}

		match, err := patternMatchAll(matchTarget, argsSlice...)
		if err != nil {
			return false, fmt.Errorf("match %q for match condition: %w", condition, err)
		}
		if match && !negate {
			continue
		}
		if match && negate {
			return false, nil
		}
		if !match && !negate {
			return false, nil
		}
	}

	// If none of the conditions explicitly return false, the match is successful
	return true, nil
}

func (s *Setter) hasProxyJump() bool {
	proxyJump, ok := s.fieldByName("ProxyJump")
	if !ok {
		return false
	}
	if proxyJump.Kind() == reflect.String {
		return proxyJump.String() != "" && proxyJump.String() != none
	} else if proxyJump.Kind() == reflect.Slice {
		for i := range proxyJump.Len() {
			if proxyJump.Index(i).String() != "" && proxyJump.Index(i).String() != none {
				return true
			}
		}
	}
	return false
}

func (s *Setter) hasProxyCommand() bool {
	proxyCommand, ok := s.fieldByName("ProxyCommand")
	if !ok {
		return false
	}
	return proxyCommand.String() != "" && proxyCommand.String() != none
}

func (s *Setter) hasProxy() bool {
	return s.hasProxyJump() || s.hasProxyCommand()
}

// determine if hostname canonicalization should be performed.
func (s *Setter) doCanonicalize() bool { //nolint:cyclop
	canonicalizeHostname, ok := s.fieldByName("CanonicalizeHostname")
	if !ok {
		return false
	}

	// CanonicalizeHostName should be set to "always" to enable hostname canonicalization when
	// a proxy is defined.
	hasProxy := s.hasProxy()

	if canonicalizeHostname.Kind() == reflect.String {
		switch canonicalizeHostname.String() {
		case "always":
			return true
		case yes:
			return !hasProxy
		default:
			return false
		}
	}

	if canonicalizeHostname.Kind() == reflect.Bool {
		return canonicalizeHostname.Bool()
	}

	var cfo options.CanonicalizeHostnameOption
	ok = false

	if canonicalizeHostname.CanInterface() {
		cfo, ok = canonicalizeHostname.Interface().(options.CanonicalizeHostnameOption)
	} else if canonicalizeHostname.CanAddr() {
		cfo, ok = canonicalizeHostname.Addr().Interface().(options.CanonicalizeHostnameOption)
	}

	if !ok {
		return false
	}

	switch cfo { //nolint:exhaustive
	case options.CanonicalizeHostnameAlways:
		return true
	case options.CanonicalizeHostnameYes:
		return !hasProxy
	default:
		return false
	}
}

// determines if canonicalizefallbacklocal is enabled.
func (s *Setter) allowCanonicalizeFallbackLocal() bool {
	canonicalizeFallbackLocal, ok := s.fieldByName("CanonicalizeFallbackLocal")
	if !ok {
		// default is yes
		return true
	}

	if canonicalizeFallbackLocal.Kind() == reflect.Bool {
		return canonicalizeFallbackLocal.Bool()
	}

	if canonicalizeFallbackLocal.Kind() == reflect.String {
		return canonicalizeFallbackLocal.String() == yes
	}

	return false
}

func (s *Setter) canonicalizeMaxDots() int {
	if md, err := s.get("CanonicalizeMaxDots", reflect.Int); err == nil {
		return int(md.Int())
	} else if md, err := s.get("CanonicalizeMaxDots", reflect.Uint); err == nil {
		return int(md.Uint())
	}
	return 1
}

func (s *Setter) canonicalDomains() []string {
	if canonicalDomains, err := s.get("CanonicalDomains", reflect.Slice, reflect.String); err == nil { //nolint:nestif
		if canonicalDomains.CanInterface() {
			if cd, ok := canonicalDomains.Interface().([]string); ok {
				return cd
			}
		}
		if canonicalDomains.CanAddr() {
			if cd, ok := canonicalDomains.Addr().Interface().([]string); ok {
				return cd
			}
		}
	}
	if canonicalDomains, err := s.get("CanonicalDomains", reflect.String); err == nil {
		return strings.Split(canonicalDomains.String(), ",")
	}
	return nil
}

func (s *Setter) canonicalizePermittedCNAMEs() map[string]string {
	res := make(map[string]string)
	if permittedCnames, err := s.get("CanonicalizePermittedCNAMEs", reflect.Slice, reflect.String); err == nil {
		if permittedCnames.Len() == 0 || (permittedCnames.Len() == 1 && permittedCnames.Index(0).String() == none) {
			return res
		}
		for i := range permittedCnames.Len() {
			parts := strings.Split(permittedCnames.Index(i).String(), ":")
			if len(parts) != 2 {
				continue
			}
			from, to := parts[0], parts[1]
			res[from] = to
		}
	}
	return res
}

// CanonicalizeHostname performs hostname canonicalization. This is done automatically
// during the finalization phase when using parser.Apply.
//
// It returns an error if the hostname couldn't be canonicalized and CanonicalizeFallbackLocal is not enabled, in which case
// you should not proceed with the connection.
func (s *Setter) CanonicalizeHostname() error { //nolint:cyclop
	if !s.doCanonicalize() {
		// CanonicalizeHostname not enabled or a proxy is defined and CanonicalizeHostname is not set to "always".
		return nil
	}

	host := s.getHost()
	if strings.HasSuffix(host, ".") {
		// Hostname is already canonical (a local host)
		return nil
	}

	if strings.Count(host, ".") > s.canonicalizeMaxDots() {
		// Enough dots as it is
		return nil
	}

	for _, domain := range s.canonicalDomains() {
		fqdn := host + "." + domain
		if _, err := net.LookupHost(fqdn); err != nil {
			continue
		}

		cname, err := net.LookupCNAME(fqdn)
		if err != nil {
			return fmt.Errorf("failed to lookup CNAME for %q: %w", fqdn, err)
		}

		// If the CNAME is different than the FQDN and permitted, return the CNAME
		if cname != fqdn { //nolint:nestif
			for from, to := range s.canonicalizePermittedCNAMEs() {
				match, err := patternMatchAll(fqdn, strings.Split(from, ",")...)
				if err != nil {
					return fmt.Errorf("failed to match fqdn %q against %q: %w", fqdn, from, err)
				}
				if !match {
					continue
				}
				match, err = patternMatchAll(cname, to)
				if err != nil {
					return fmt.Errorf("failed to match cname %q against %q: %w", fqdn, to, err)
				}
				if match {
					if err := s.setHost(fkHost, cname); err != nil {
						return fmt.Errorf("failed to set host to %q: %w", cname, err)
					}
					return nil
				}
			}
		}
		// No acceptable CNAME, return the FQDN
		if err := s.setHost(fkHost, fqdn); err != nil {
			return fmt.Errorf("failed to set host to %q: %w", cname, err)
		}
		return nil
	}

	// No fqdn or cname found

	if s.allowCanonicalizeFallbackLocal() {
		// Fallback to the original is permitted.
		return nil
	}

	// Fallback to the original is not permitted.
	return fmt.Errorf("%w: failed to canonicalize %q and CanonicalizeFallbackLocal is not enabled", errCanonicalizationFailed, host)
}

// The Match condition "localnetwork" matches a list of cidrs against
// ip addresses on local network interfaces.
func matchLocalNetwork(cidrs []string) (bool, error) {
	parsedCIDRs := make([]*net.IPNet, len(cidrs))
	for i, cidr := range cidrs {
		_, parsedCIDR, err := net.ParseCIDR(cidr)
		if err != nil {
			return false, fmt.Errorf("invalid CIDR %s: %w", cidr, err)
		}
		parsedCIDRs[i] = parsedCIDR
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return false, fmt.Errorf("can't get network interfaces: %w", err)
	}
	for _, iface := range interfaces {
		addrs, err := iface.Addrs()
		if err != nil {
			return false, fmt.Errorf("can't get addresses for %s: %w", iface.Name, err)
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			// Check if IP is within any of the CIDRs
			for _, cidr := range parsedCIDRs {
				if cidr.Contains(ip) {
					// match found
					return true, nil
				}
			}
		}
	}
	// No match found
	return false, nil
}
