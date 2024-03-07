package kv

import (
	"bufio"
	"encoding"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
)

var (
	// ErrInvalidTags is returned when the struct tags are invalid for kv.
	ErrInvalidTags = errors.New("invalid tags")

	// ErrInvalidObject is returned when the object is invalid to be a kv decoding target.
	ErrInvalidObject = errors.New("invalid object")

	// ErrStrict is returned when the decoder is in strict mode and an unknown key is encountered.
	ErrStrict = errors.New("strict mode")
)

type assigner interface {
	assign(key, value string) error
}

type mapAssigner struct {
	m map[string]string
}

func (ma *mapAssigner) assign(key, value string) error {
	ma.m[key] = value
	return nil
}

type fieldInfo struct {
	key   string
	name  string
	value reflect.Value
	field reflect.StructField

	ignore     bool
	catchAll   bool
	omitempty  bool
	ignorecase bool
	delim      rune
	err        error
}

func newFieldInfo(field reflect.Value, structField reflect.StructField) fieldInfo { //nolint:cyclop
	kvTag := structField.Tag.Get("kv")
	parts := strings.Split(kvTag, ",")

	info := fieldInfo{
		value: field,
		field: structField,
		name:  structField.Name,
	}

	if len(parts) == 0 {
		return info
	}
	switch parts[0] {
	case "-":
		info.ignore = true
	case "*":
		info.catchAll = true
	default:
		info.key = parts[0]
	}

	if len(parts) > 1 {
		for _, part := range parts[1:] {
			switch {
			case part == "omitempty":
				info.omitempty = true
			case part == "ignorecase":
				info.ignorecase = true
			case strings.HasPrefix(part, "delim:"), strings.HasPrefix(part, "delim="):
				if len(part) > 6 {
					delimRunes := []rune(part[6:])
					if len(delimRunes) > 0 {
						info.delim = delimRunes[0]
					}
				}
			}
		}
	}
	if err := checkField(field); err != nil {
		info.err = err
	}
	return info
}

type reflectAssigner struct {
	obj      any
	catchAll assigner
	fields   []fieldInfo
	strict   bool
}

func checkField(field reflect.Value) error {
	if !field.CanSet() {
		return fmt.Errorf("%w: field s unexported", ErrInvalidTags)
	}

	for _, kind := range getSupportedKinds() {
		if field.Kind() == kind {
			return nil
		}
	}

	return fmt.Errorf("%w: unsupported type %v", ErrInvalidTags, field.Kind())
}

func getSupportedKinds() []reflect.Kind {
	return []reflect.Kind{
		reflect.String,
		reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64,
		reflect.Slice,
		reflect.Ptr,
	}
}

func (ra *reflectAssigner) setup() error {
	val := reflect.ValueOf(ra.obj)
	if val.Kind() != reflect.Ptr || val.IsNil() {
		return fmt.Errorf("%w: object must be a non-nil pointer", ErrInvalidObject)
	}

	elem := val.Elem()
	if elem.Kind() != reflect.Struct {
		return fmt.Errorf("%w: object must be a pointer to a struct", ErrInvalidObject)
	}

	typ := elem.Type()
	for i := 0; i < elem.NumField(); i++ {
		field := elem.Field(i)
		structField := typ.Field(i)
		ra.fields = append(ra.fields, newFieldInfo(field, structField))
	}

	for _, info := range ra.fields {
		switch {
		case info.catchAll:
			if ra.catchAll != nil {
				return fmt.Errorf("%w: multiple fields with kv tag *", ErrInvalidTags)
			}
			if err := ra.setupCatchAll(info.value); err != nil {
				return fmt.Errorf("setup catch all field %s: %w", info.name, err)
			}
		case info.key != "":
			if err := checkField(info.value); err != nil {
				return fmt.Errorf("field %s: %w", info.name, err)
			}
		}
	}

	return nil
}

func (ra *reflectAssigner) setupCatchAll(field reflect.Value) error {
	if field.Kind() != reflect.Map {
		return fmt.Errorf("%w: field with kv tag * must be a map", ErrInvalidTags)
	}
	if field.IsNil() {
		field.Set(reflect.MakeMap(field.Type()))
	}
	mapObj, ok := field.Interface().(map[string]string)
	if !ok {
		return fmt.Errorf("%w: field with kv tag * must be a map of string to string", ErrInvalidTags)
	}
	ra.catchAll = &mapAssigner{m: mapObj}
	return nil
}

func parseBool(str string) bool {
	switch str {
	case "1", "true", "TRUE", "True", "on", "ON", "On", "yes", "YES", "Yes", "y", "Y":
		return true
	default:
		return false
	}
}

func (ra *reflectAssigner) assignPtr(info fieldInfo, value string) error {
	field := info.value
	if field.IsNil() {
		newElem := reflect.New(field.Type().Elem())
		field.Set(newElem)
	}

	// Dereference the pointer to get the actual value to assign to
	elem := field.Elem()
	info.value = elem

	if err := ra.assignField(info, value); err != nil {
		return err
	}

	return nil
}

func (ra *reflectAssigner) assignSlice(info fieldInfo, value string) error {
	delim := ","
	if info.delim != 0 {
		delim = string(info.delim)
	}
	field := info.value

	parts := strings.Split(value, delim)

	for _, part := range parts {
		// Create a new addressable (modifiable) value for the slice's element type
		newElemPtr := reflect.New(field.Type().Elem())
		newElem := newElemPtr.Elem()

		// Use assignField to set the value of the new element
		tempInfo := info
		tempInfo.value = newElem
		if err := ra.assignField(tempInfo, strings.TrimSpace(part)); err != nil {
			return err
		}

		// Append the new element to the slice
		field.Set(reflect.Append(field, newElem))
	}

	return nil
}

func (ra *reflectAssigner) assignField(info fieldInfo, value string) error { //nolint:cyclop
	if info.ignore {
		return nil
	}
	if info.err != nil {
		return fmt.Errorf("field %q can not be used with kv: %w", info.name, info.err)
	}

	field := info.value

	if field.CanInterface() {
		if f, ok := field.Interface().(encoding.TextUnmarshaler); ok {
			if err := f.UnmarshalText([]byte(value)); err != nil {
				return fmt.Errorf("unmarshal text: %w", err)
			}
			return nil
		}
	}

	switch field.Kind() { //nolint:exhaustive
	case reflect.String:
		field.SetString(value)
	case reflect.Bool:
		field.SetBool(parseBool(value))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("parse int: %w", err)
		}
		field.SetInt(int64(i))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		i, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return fmt.Errorf("parse uint: %w", err)
		}
		field.SetUint(i)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("parse float: %w", err)
		}
		field.SetFloat(f)
	case reflect.Ptr:
		if err := ra.assignPtr(info, value); err != nil {
			return fmt.Errorf("assign ptr: %w", err)
		}
	case reflect.Slice:
		if err := ra.assignSlice(info, value); err != nil {
			return fmt.Errorf("assign to slice: %w", err)
		}
	default:
		return fmt.Errorf("%w: unsupported field type %v", ErrInvalidObject, field.Kind())
	}

	return nil
}

func (ra *reflectAssigner) getInfo(key string) (fieldInfo, bool) {
	if key == "" {
		return fieldInfo{}, false
	}
	for _, info := range ra.fields {
		if info.key == key {
			return info, true
		}
		if info.ignorecase && strings.EqualFold(info.key, key) {
			return info, true
		}
		if info.name == key {
			return info, true
		}
		if info.ignorecase && strings.EqualFold(info.name, key) {
			return info, true
		}
	}
	return fieldInfo{}, false
}

func (ra *reflectAssigner) assign(key, value string) error {
	info, ok := ra.getInfo(key)
	if !ok {
		if ra.catchAll != nil {
			if err := ra.catchAll.assign(key, value); err != nil {
				return fmt.Errorf("assign to catch all: %w", err)
			}
		}
		if ra.strict {
			return fmt.Errorf("%w: unknown field for key %q", ErrStrict, key)
		}
		return nil
	}
	if err := ra.assignField(info, value); err != nil {
		return fmt.Errorf("assign field %q: %w", key, err)
	}
	return nil
}

// Decoder is a decoder for key-value pairs, similar to the encoding/json package.
// You can use struct tags to customize the decoding process.
//
// The tag format is `kv:"key,option1,option2"`.
//
// The key is the key in the key-value pair. If the key is empty, the field name is used.
// If the key is "-", the field is ignored.
// If the key is "*", the field is a map of string to string and will catch all keys that are not explicitly defined.
//
// The options are:
// - ignorecase: the key is matched case-insensitively
// - delim: when the field is a slice, the value is split by the specified delimiter.
type Decoder struct {
	r            io.Reader
	rdelim       byte
	fdelim       rune
	commentstart string
	assigner     assigner
	strict       bool
}

// NewDecoder returns a new decoder that reads from r.
func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{r: r, rdelim: '\n', fdelim: '=', commentstart: "#"}
}

// FieldDelimiter sets the field delimiter. The default is '='.
func (d *Decoder) FieldDelimiter(delim rune) {
	d.fdelim = delim
}

// RowDelimiter sets the row delimiter. The default is '\n'.
func (d *Decoder) RowDelimiter(delim byte) {
	d.rdelim = delim
}

// CommentStart sets the comment start string. If a line starts with the comment start string, it is ignored.
func (d *Decoder) CommentStart(comment string) {
	d.commentstart = comment
}

// Strict makes the decoder return an error when an unknown key or an assign error is encountered.
func (d *Decoder) Strict() {
	d.strict = true
}

func (d *Decoder) setAssigner(obj any) error {
	switch v := obj.(type) {
	case map[string]string:
		if v == nil {
			return fmt.Errorf("%w: map must be non-nil", ErrInvalidObject)
		}
		d.assigner = &mapAssigner{m: v}
	default:
		ra := &reflectAssigner{obj: obj, strict: d.strict}
		if err := ra.setup(); err != nil {
			return err
		}
		d.assigner = ra
	}
	return nil
}

// Decode reads all of the key-value pairs from the input and stores them in the map or struct pointed to by obj.
func (d *Decoder) Decode(obj any) (err error) {
	defer func() {
		if r := recover(); r != nil {
			// Capture the panic, optionally log it
			err = fmt.Errorf("panic in Decode: %v", r) //nolint:goerr113
		}
	}()

	if err := d.setAssigner(obj); err != nil {
		return err
	}
	reader := bufio.NewReader(d.r)
	for {
		line, readErr := reader.ReadString(d.rdelim)
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return fmt.Errorf("read: %w", readErr)
		}

		line = strings.TrimSpace(line)

		if len(line) == 0 {
			continue
		}

		if d.commentstart != "" && strings.HasPrefix(line, d.commentstart) {
			continue
		}

		k, v, err := SplitRune(line, d.fdelim)
		if err != nil {
			return fmt.Errorf("split: %w", err)
		}
		if err := d.assigner.assign(k, v); err != nil {
			if d.strict {
				return fmt.Errorf("assign: %w", err)
			}
		}
	}
}
