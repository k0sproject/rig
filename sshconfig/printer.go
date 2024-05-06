package sshconfig

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Dump an SSH configuration to a string.
//
// Example:
//
//	config, _ := sshconfig.ConfigFor("example.com")
//	dump, _ := sshconfig.Dump(config)
//	fmt.Println(dump)
//	// Output:
//	// Host example.com
//	// 	Hostname example.com
//	// 	User user
//	// 	..and so on
func Dump(obj any) (string, error) {
	p, err := newprinter(obj)
	if err != nil {
		return "", fmt.Errorf("can't create printer: %w", err)
	}
	return p.dump(), nil
}

type printer struct {
	setter *Setter
}

func newprinter(obj any) (*printer, error) {
	setter, err := NewSetter(obj)
	if err != nil {
		return nil, fmt.Errorf("can't create a setter for importing field info: %w", err)
	}
	p := &printer{
		setter: setter,
	}
	return p, nil
}

func (p *printer) fieldByName(key string) (reflect.Value, bool) {
	return p.setter.fieldByName(key)
}

func (p *printer) get(key string, expectedKinds ...reflect.Kind) (reflect.Value, error) {
	return p.setter.get(key, expectedKinds...)
}

func quote(s string) string {
	if s == "" {
		return ""
	}
	if strings.Contains(s, " ") {
		return strconv.Quote(s)
	}
	return s
}

func (p *printer) stringify(field reflect.Value) (string, bool) {
	if field.Kind() == reflect.String {
		return quote(field.String()), true
	}
	if field.Kind() == reflect.Ptr && field.Elem().Kind() == reflect.String {
		return quote(field.Elem().String()), true
	}
	if field.CanInterface() {
		if s, ok := field.Interface().(fmt.Stringer); ok {
			return quote(s.String()), true
		}
	}
	if field.CanAddr() {
		if s, ok := field.Addr().Interface().(fmt.Stringer); ok {
			return quote(s.String()), true
		}
	}
	return "", false
}

func (p *printer) stringer(key string) (string, bool) {
	field, ok := p.fieldByName(key)
	if !ok {
		return "", false
	}
	return p.stringify(field)
}

func (p *printer) stringerslicejoin(key string, separator rune) (string, bool) {
	field, err := p.get(key, reflect.Slice)
	if err != nil {
		return "", false
	}
	if field.Len() == 0 {
		return "", false
	}
	sb := &strings.Builder{}
	for i := range field.Len() {
		if i > 0 {
			sb.WriteRune(separator)
		}
		if s, ok := p.stringify(field.Index(i)); ok {
			if strings.HasSuffix(key, "File") && s[1] == ':' {
				// Windows path with a drive letter
				s = strings.ReplaceAll(s, "/", "\\")
			}
			sb.WriteString(s)
		}
	}
	return sb.String(), true
}

func (p *printer) stringerslice(key string) (string, bool) {
	return p.stringerslicejoin(key, ' ')
}

func (p *printer) stringercsv(key string) (string, bool) {
	return p.stringerslicejoin(key, ',')
}

func (p *printer) number(key string) (string, bool) {
	if field, err := p.get(key, reflect.Int); err == nil {
		return strconv.FormatInt(field.Int(), 10), true
	}
	if field, err := p.get(key, reflect.Uint); err == nil {
		if key == "StreamLocalBindMask" {
			return "0" + strconv.FormatInt(int64(field.Uint()), 8), true
		}
		return strconv.FormatInt(int64(field.Uint()), 10), true
	}
	return "", false
}

func (p *printer) boolean(key string) (string, bool) {
	if field, err := p.get(key, reflect.Bool); err == nil {
		if field.Bool() {
			return yes, true
		}
		return no, true
	}

	if field, err := p.get(key, reflect.String); err == nil {
		return field.String(), true
	}

	return "", false
}

func (p *printer) duration(key string) (string, bool) {
	kind := reflect.TypeOf(time.Duration(0)).Kind()
	if field, err := p.get(key, kind); err == nil {
		if field.CanInterface() {
			if d, ok := field.Interface().(time.Duration); ok {
				return strconv.Itoa(int(d.Seconds())), true
			}
		}
	}

	if field, err := p.get(key, reflect.String); err == nil {
		return field.String(), true
	}

	return "", false
}

func (p *printer) channeltimeout(key string) (string, bool) {
	kind := reflect.TypeOf(map[string]time.Duration{}).Kind()
	if field, err := p.get(key, kind); err == nil { //nolint:nestif
		if field.CanInterface() {
			if d, ok := field.Interface().(map[string]time.Duration); ok {
				sb := &strings.Builder{}
				for k, v := range d {
					if sb.Len() > 0 {
						sb.WriteRune(' ')
					}
					sb.WriteString(k)
					sb.WriteRune('=')
					sb.WriteString(strconv.Itoa(int(v.Seconds())))
				}
				return sb.String(), true
			}
		}
	}
	if field, err := p.get(key, reflect.Slice); err == nil {
		sb := &strings.Builder{}
		for i := range field.Len() {
			if s, ok := p.stringify(field.Index(i)); ok {
				if sb.Len() > 0 {
					sb.WriteRune(' ')
				}
				sb.WriteString(s)
			}
		}
		return sb.String(), true
	}
	if field, err := p.get(key, reflect.String); err == nil {
		return field.String(), true
	}
	return "", false
}

func (p *printer) forward(key string) (string, bool) {
	kind := reflect.TypeOf(map[string]string{}).Kind()
	if field, err := p.get(key, kind); err == nil { //nolint:nestif
		if field.CanInterface() {
			if d, ok := field.Interface().(map[string]string); ok {
				sb := &strings.Builder{}
				for k, v := range d {
					if sb.Len() > 0 {
						sb.WriteRune(' ')
					}
					sb.WriteString(k)
					sb.WriteRune(' ')
					sb.WriteString(v)
				}
				return sb.String(), true
			}
		}
	}
	if field, err := p.get(key, reflect.Slice); err == nil {
		sb := &strings.Builder{}
		for i := range field.Len() {
			if s, ok := p.stringify(field.Index(i)); ok {
				if sb.Len() > 0 {
					sb.WriteRune(' ')
				}
				sb.WriteString(s)
			}
		}
	}
	return "", false
}

func (p *printer) dump() string { //nolint:cyclop
	sb := &strings.Builder{}
	keys := make([]string, len(knownKeys))
	i := 0
	for key := range knownKeys {
		keys[i] = key
		i++
	}
	sort.Strings(keys)
	host := p.setter.getHost()
	if host != "" {
		sb.WriteString("Host ")
		sb.WriteString(host)
		sb.WriteRune('\n')
	}
	for _, key := range keys {
		if key == "Host" {
			continue
		}
		keyinfo, ok := knownKeys[key]
		if !ok {
			continue
		}
		if keyinfo.printFunc == nil {
			continue
		}
		if key == "localforward" || key == "remoteforward" { //nolint:nestif
			slice, ok := p.stringerslice(key)
			if !ok {
				continue
			}
			if slice == "" {
				continue
			}
			for i, s := range strings.Split(slice, " ") {
				if i%2 == 0 {
					sb.WriteRune('\t')
					sb.WriteString(key)
					sb.WriteRune(' ')
				}
				sb.WriteString(s)
				if i%2 != 0 {
					sb.WriteRune(' ')
				} else {
					sb.WriteRune('\n')
				}
			}
			continue
		}
		strval, ok := keyinfo.printFunc(p, keyinfo.key)
		if !ok {
			continue
		}
		if strval == "" {
			continue
		}
		sb.WriteRune('\t')
		sb.WriteString(keyinfo.key)
		sb.WriteRune(' ')
		sb.WriteString(strval)
		sb.WriteRune('\n')
	}
	return sb.String()
}
