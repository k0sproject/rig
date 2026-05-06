package kv

import (
	"encoding"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// Encoder writes key-value pairs to an output stream. The field delimiter
// defaults to '=' and the row delimiter to '\n'.
//
// Struct fields are encoded using the same kv struct tags as Decoder:
//
//	Field type `kv:"key_name,option1,option2"`
//
// Tag options:
//   - omitempty: skip the field when its value is the zero value
//   - delim=X: for slice fields, join elements with delimiter X (default ',')
//   - '-': skip the field entirely
//   - '*': encode a map[string]string field as additional key-value pairs
//
// Keys and values that contain the field delimiter, quotes, backslashes,
// leading/trailing whitespace, or keys that start with the comment prefix
// (default "#") are automatically double-quoted with backslash escaping.
// Keys or values containing the row delimiter cannot be encoded and cause an error.
// Note: slice elements that themselves contain the slice delimiter or
// leading/trailing whitespace cannot be round-tripped through the Decoder.
type Encoder struct {
	w            io.Writer
	rdelim       byte
	fdelim       rune
	commentStart string
}

// NewEncoder returns a new Encoder that writes to w.
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: w, rdelim: '\n', fdelim: '=', commentStart: "#"}
}

// CommentStart sets the comment prefix. Keys starting with this prefix are
// automatically quoted so they are not mistaken for comments by Decoder.
// Default is "#" to match Decoder's default.
func (e *Encoder) CommentStart(s string) { e.commentStart = s }

// RowDelimiter sets the row delimiter. Default is newline.
func (e *Encoder) RowDelimiter(delim byte) { e.rdelim = delim }

// FieldDelimiter sets the field delimiter. Default is '='.
func (e *Encoder) FieldDelimiter(delim rune) { e.fdelim = delim }

type encFieldInfo struct {
	key       string
	ignore    bool
	catchAll  bool
	omitempty bool
	delim     rune
}

func parseEncTag(sf reflect.StructField) encFieldInfo {
	info := encFieldInfo{}
	parts := strings.Split(sf.Tag.Get("kv"), ",")
	switch parts[0] {
	case "-":
		info.ignore = true
		return info
	case "*":
		info.catchAll = true
	default:
		info.key = parts[0]
	}
	for _, opt := range parts[1:] {
		switch {
		case opt == "omitempty":
			info.omitempty = true
		case strings.HasPrefix(opt, "delim:"), strings.HasPrefix(opt, "delim="):
			if rs := []rune(opt[6:]); len(rs) > 0 {
				info.delim = rs[0]
			}
		}
	}
	return info
}

func fieldToStringSlice(value reflect.Value, delim rune) (string, error) {
	sep := ","
	if delim != 0 {
		sep = string(delim)
	}
	parts := make([]string, value.Len())
	for i := range value.Len() {
		s, err := fieldToString(value.Index(i), delim)
		if err != nil {
			return "", err
		}
		parts[i] = s
	}
	return strings.Join(parts, sep), nil
}

func fieldToStringNumeric(value reflect.Value) (string, error) {
	switch value.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(value.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(value.Uint(), 10), nil
	case reflect.Float32:
		return strconv.FormatFloat(value.Float(), 'g', -1, 32), nil
	case reflect.Float64:
		return strconv.FormatFloat(value.Float(), 'g', -1, 64), nil
	default:
		return "", fmt.Errorf("%w: unsupported type %v", ErrInvalidObject, value.Kind())
	}
}

func marshalText(value reflect.Value) (string, bool, error) {
	if value.Kind() == reflect.Pointer && value.IsNil() {
		return "", false, nil
	}
	// Try value receiver first, then pointer receiver for addressable values.
	for _, v := range []reflect.Value{value, addrOf(value)} {
		if !v.IsValid() || !v.CanInterface() {
			continue
		}
		m, ok := v.Interface().(encoding.TextMarshaler)
		if !ok {
			continue
		}
		b, err := m.MarshalText()
		if err != nil {
			return "", false, fmt.Errorf("marshal text: %w", err)
		}
		return string(b), true, nil
	}
	return "", false, nil
}

func addrOf(v reflect.Value) reflect.Value {
	if v.CanAddr() {
		return v.Addr()
	}
	// Allocate an addressable copy so pointer-receiver TextMarshaler
	// implementations work for non-addressable values such as map entries.
	ptr := reflect.New(v.Type())
	ptr.Elem().Set(v)
	return ptr
}

func fieldToString(value reflect.Value, delim rune) (string, error) {
	if s, ok, err := marshalText(value); ok || err != nil {
		return s, err
	}
	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			return "", nil
		}
		return fieldToString(value.Elem(), delim)
	case reflect.Slice:
		return fieldToStringSlice(value, delim)
	case reflect.String:
		return value.String(), nil
	case reflect.Bool:
		if value.Bool() {
			return "true", nil
		}
		return "false", nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return fieldToStringNumeric(value)
	default:
		return "", fmt.Errorf("%w: unsupported type %v", ErrInvalidObject, value.Kind())
	}
}

func isZeroValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Map:
		return v.IsNil()
	default:
		return v.IsZero()
	}
}

func (e *Encoder) needsQuoting(s string) bool {
	if strings.TrimSpace(s) != s {
		return true
	}
	if e.commentStart != "" && strings.HasPrefix(s, e.commentStart) {
		return true
	}
	for _, r := range s {
		if r == '\\' || r == '"' || r == '\'' || r == e.fdelim {
			return true
		}
	}
	return false
}

// quoteIfNeeded wraps s in double quotes and escapes backslashes and double
// quotes if s contains any character that would be misinterpreted by Decoder.
func (e *Encoder) quoteIfNeeded(s string) string {
	if !e.needsQuoting(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for _, c := range s {
		if c == '\\' || c == '"' {
			b.WriteByte('\\')
		}
		b.WriteRune(c)
	}
	b.WriteByte('"')
	return b.String()
}

func (e *Encoder) writePair(key, value string) error {
	if key == "" {
		return fmt.Errorf("%w: empty key", ErrInvalidObject)
	}
	if strings.IndexByte(key, e.rdelim) >= 0 || strings.IndexByte(value, e.rdelim) >= 0 {
		return fmt.Errorf("%w: key or value contains the row delimiter", ErrInvalidObject)
	}
	_, err := fmt.Fprintf(e.w, "%s%c%s%c", e.quoteIfNeeded(key), e.fdelim, e.quoteIfNeeded(value), e.rdelim)
	if err != nil {
		return fmt.Errorf("write pair: %w", err)
	}
	return nil
}

type encodedPair struct{ key, val string }

func (e *Encoder) encodeMap(v reflect.Value) error {
	pairs := make([]encodedPair, 0, v.Len())
	for _, k := range v.MapKeys() {
		kStr, err := fieldToString(k, 0)
		if err != nil {
			return err
		}
		vStr, err := fieldToString(v.MapIndex(k), 0)
		if err != nil {
			return err
		}
		pairs = append(pairs, encodedPair{kStr, vStr})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].key < pairs[j].key })
	for _, p := range pairs {
		if err := e.writePair(p.key, p.val); err != nil {
			return err
		}
	}
	return nil
}

func (e *Encoder) encodeField(structField reflect.StructField, fieldValue reflect.Value, info encFieldInfo) error {
	if info.catchAll {
		m, ok := fieldValue.Interface().(map[string]string)
		if !ok {
			return fmt.Errorf("%w: field %q with kv tag * must be a map[string]string", ErrInvalidTags, structField.Name)
		}
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if err := e.writePair(k, m[k]); err != nil {
				return err
			}
		}
		return nil
	}
	if info.omitempty && isZeroValue(fieldValue) {
		return nil
	}
	key := info.key
	if key == "" {
		key = structField.Name
	}
	val, err := fieldToString(fieldValue, info.delim)
	if err != nil {
		return fmt.Errorf("field %q: %w", key, err)
	}
	return e.writePair(key, val)
}

func (e *Encoder) encodeStruct(structVal reflect.Value) error {
	t := structVal.Type()
	catchAllSeen := false
	for i := range t.NumField() {
		structField := t.Field(i)
		if !structField.IsExported() {
			if tag := structField.Tag.Get("kv"); tag != "" && tag != "-" {
				return fmt.Errorf("%w: unexported field %q has kv tag", ErrInvalidTags, structField.Name)
			}
			continue
		}
		fieldValue := structVal.Field(i)
		info := parseEncTag(structField)
		if info.ignore {
			continue
		}
		if info.catchAll {
			if catchAllSeen {
				return fmt.Errorf("%w: multiple fields with kv tag *", ErrInvalidTags)
			}
			catchAllSeen = true
		}
		if err := e.encodeField(structField, fieldValue, info); err != nil {
			return err
		}
	}
	return nil
}

// Encode writes the key-value representation of obj to the stream.
// obj may be a map with string-convertible keys, or a struct (or pointer to struct).
func (e *Encoder) Encode(obj any) error {
	value := reflect.ValueOf(obj)
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return fmt.Errorf("%w: nil pointer", ErrInvalidObject)
		}
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.Map:
		return e.encodeMap(value)
	case reflect.Struct:
		if !value.CanAddr() {
			// Heap-allocate a copy so fields are addressable, enabling
			// pointer-receiver TextMarshaler implementations on struct fields.
			ptr := reflect.New(value.Type())
			ptr.Elem().Set(value)
			value = ptr.Elem()
		}
		return e.encodeStruct(value)
	default:
		return fmt.Errorf("%w: unsupported type %T", ErrInvalidObject, obj)
	}
}
