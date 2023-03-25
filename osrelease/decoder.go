package osrelease

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
)

const tag = "osrelease"

var fieldsByTag map[string]string // lookup table for reflect to get fields by tag

// ErrParseOSRelease is returned when an error occurs parsing an os-release file
var ErrParseOSRelease = errors.New("parse osrelease")

// Decode decodes an os-release file from an io.Reader
func Decode(r io.Reader) (*OSRelease, error) {
	osr := &OSRelease{}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := parts[0]
		value := parts[1]
		if v, err := strconv.Unquote(value); err == nil {
			value = v
		}

		setField(osr, key, value)
	}
	if err := scanner.Err(); err != nil {
		return nil, errors.Join(ErrParseOSRelease, err)
	}

	// ArchLinux has no versions
	if osr.ID == "arch" || osr.IDLike == "arch" {
		osr.Version = "0.0.0"
	}

	if osr.Name == "" || osr.ID == "" {
		return nil, fmt.Errorf("%w: missing required fields", ErrParseOSRelease)
	}

	return osr, nil
}

// DecodeString decodes an os-release file from a string
func DecodeString(s string) (*OSRelease, error) {
	return Decode(strings.NewReader(s))
}

func setField(osr *OSRelease, key, value string) {
	if fieldsByTag == nil {
		fieldsByTag = buildTable()
	}

	fn, ok := fieldsByTag[key]
	if !ok {
		if osr.Extra == nil {
			osr.Extra = make(map[string]string)
		}
		osr.Extra[key] = value
		return
	}

	field, ok := reflect.TypeOf(osr).Elem().FieldByName(fn)
	if !ok {
		return
	}

	f := reflect.ValueOf(osr).Elem().FieldByName(field.Name)
	if !f.CanSet() {
		return
	}
	f.SetString(value)
}

func buildTable() map[string]string {
	table := make(map[string]string)
	rt := reflect.TypeOf(OSRelease{})

	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		v := f.Tag.Get(tag)
		table[v] = f.Name
	}

	return table
}
