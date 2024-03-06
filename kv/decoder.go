package kv

import (
	"bufio"
	"context"
	"encoding"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"strconv"
	"strings"

	"github.com/k0sproject/rig/log"
)

type assigner interface {
	assign(key, value string) error
}

type mapAssigner struct {
	m map[string]string
}

func (ma *mapAssigner) assign(key, value string) error {
	log.Trace(context.Background(), "kv decoder: assigning to map", slog.String("key", key), slog.String("value", value))
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
		return errors.New("field is unexported")
	}

	for _, kind := range getSupportedKinds() {
		if field.Kind() == kind {
			return nil
		}
	}

	return fmt.Errorf("unsupported type %v", field.Kind())
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
		return errors.New("object must be a non-nil pointer")
	}

	elem := val.Elem()
	if elem.Kind() != reflect.Struct {
		return errors.New("object must be a pointer to a struct")
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
				return errors.New("multiple fields with kv tag *")
			}
			if err := ra.setupCatchAll(info.value); err != nil {
				return fmt.Errorf("field %s: %v", info.name, err)
			}
		case info.key != "":
			if err := checkField(info.value); err != nil {
				return fmt.Errorf("field %s: %v", info.name, err)
			}
		}
	}

	return nil
}

func (ra *reflectAssigner) setupCatchAll(field reflect.Value) error {
	if field.Kind() != reflect.Map {
		return errors.New("field with kv tag * must be a map")
	}
	if field.IsNil() {
		field.Set(reflect.MakeMap(field.Type()))
	}
	mapObj, ok := field.Interface().(map[string]string)
	if !ok {
		return errors.New("field with kv tag * must be a map of string to string")
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
	log.Trace(context.Background(), "kv decoder: assigning to slice", slog.String("field", info.name), slog.String("value", value))
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

func (ra *reflectAssigner) assignField(info fieldInfo, value string) error {
	log.Trace(context.Background(), "kv decoder: assigning field", slog.String("field", info.name), slog.String("value", value))
	if info.ignore {
		log.Trace(context.Background(), "kv decoder: field is ignored", slog.String("field", info.name))
		return nil
	}
	if info.err != nil {
		return fmt.Errorf("field %q can not be used with kv: %w", info.name, info.err)
	}

	field := info.value

	if field.CanInterface() {
		if f, ok := field.Interface().(encoding.TextUnmarshaler); ok {
			log.Trace(context.Background(), "kv decoder: field is a text unmarshaler", slog.String("field", info.name))
			return f.UnmarshalText([]byte(value))
		}
	}

	switch field.Kind() {
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
		return fmt.Errorf("unsupported field type %v", field.Kind())
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
			log.Trace(context.Background(), "kv decoder: assigning to catch all", slog.String("key", key), slog.String("value", value))
			return ra.catchAll.assign(key, value)
		}
		if ra.strict {
			return fmt.Errorf("unknown field for key %q", key)
		}
		return nil
	}
	if err := ra.assignField(info, value); err != nil {
		return fmt.Errorf("assign field %q: %v", key, err)
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
// - delim: when the field is a slice, the value is split by the specified delimiter
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

func (d *Decoder) FieldDelimiter(delim rune) {
	d.fdelim = delim
}

func (d *Decoder) RowDelimiter(delim byte) {
	d.rdelim = delim
}

func (d *Decoder) CommentStart(comment string) {
	d.commentstart = comment
}

func (d *Decoder) Strict() {
	d.strict = true
}

func (d *Decoder) setAssigner(obj any) error {
	switch v := obj.(type) {
	case map[string]string:
		if v == nil {
			return errors.New("map must be non-nil")
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
			err = fmt.Errorf("panic in Decode: %v", r)
		}
	}()

	if err := d.setAssigner(obj); err != nil {
		return err
	}
	reader := bufio.NewReader(d.r)
	for {
		line, readErr := reader.ReadString(d.rdelim)
		if readErr != nil {
			log.Trace(context.Background(), "kv decoder: readstring returned an error", log.ErrorAttr(readErr))
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
