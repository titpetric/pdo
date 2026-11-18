package client

import (
	"fmt"
	"reflect"
	"strings"
)

// compileNamed turns `:name` placeholders into `?` and returns the names in
// the order they appear. `::` is an escaped colon (a postgres cast stays
// `::`), and a colon inside a quoted string is left alone.
func compileNamed(query string) (string, []string) {
	var (
		out   strings.Builder
		names []string
	)
	out.Grow(len(query))

	for i := 0; i < len(query); i++ {
		c := query[i]
		switch {
		case c == '\'' || c == '"' || c == '`':
			end := strings.IndexByte(query[i+1:], c)
			if end < 0 {
				out.WriteString(query[i:])
				return out.String(), names
			}
			out.WriteString(query[i : i+end+2])
			i += end + 1
		case c == ':' && i+1 < len(query) && query[i+1] == ':':
			out.WriteString("::")
			i++
		case c == ':' && i+1 < len(query) && isNameByte(query[i+1]):
			j := i + 1
			for j < len(query) && isNameByte(query[j]) {
				j++
			}
			names = append(names, query[i+1:j])
			out.WriteByte('?')
			i = j - 1
		default:
			out.WriteByte(c)
		}
	}
	return out.String(), names
}

func isNameByte(c byte) bool {
	return c == '_' || c == '.' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// bindNamedArgs expands query against a struct or string-keyed map into a
// positional `?` statement and its arguments.
func bindNamedArgs(query string, arg any) (string, []any, error) {
	q, names := compileNamed(query)
	if len(names) == 0 {
		return q, nil, nil
	}

	lookup, err := namedLookup(arg)
	if err != nil {
		return "", nil, err
	}

	args := make([]any, len(names))
	for i, name := range names {
		v, ok := lookup(name)
		if !ok {
			return "", nil, fmt.Errorf("pdodb: could not find name %s in %T", name, arg)
		}
		args[i] = v
	}
	return q, args, nil
}

func namedLookup(arg any) (func(string) (any, bool), error) {
	switch m := arg.(type) {
	case map[string]any:
		return func(name string) (any, bool) { v, ok := m[name]; return v, ok }, nil
	case map[string]string:
		return func(name string) (any, bool) { v, ok := m[name]; return v, ok }, nil
	}

	v := reflect.ValueOf(arg)
	for v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil, fmt.Errorf("pdodb: nil value %T", arg)
		}
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			return nil, fmt.Errorf("pdodb: map key must be a string, got %T", arg)
		}
		return func(name string) (any, bool) {
			e := v.MapIndex(reflect.ValueOf(name).Convert(v.Type().Key()))
			if !e.IsValid() {
				return nil, false
			}
			return e.Interface(), true
		}, nil
	case reflect.Struct:
		fields := fieldsOf(v.Type())
		return func(name string) (any, bool) {
			idx, ok := fields[name]
			if !ok {
				return nil, false
			}
			f, ok := fieldByIndexReadOnly(v, idx)
			if !ok {
				return nil, true
			}
			return f.Interface(), true
		}, nil
	}
	return nil, fmt.Errorf("pdodb: named arguments must be a struct or map, got %T", arg)
}
