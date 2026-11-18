package client

import (
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"sync"
)

var scannerType = reflect.TypeOf((*sql.Scanner)(nil)).Elem()

// fieldCache holds the column to field index map of each struct type.
var fieldCache sync.Map // reflect.Type -> map[string][]int

// fieldsOf maps column names to field index paths for a struct type. The name
// is the `db` tag, or the lowercased field name when untagged; embedded
// structs are flattened and `db:"-"` skips a field.
func fieldsOf(t reflect.Type) map[string][]int {
	if cached, ok := fieldCache.Load(t); ok {
		return cached.(map[string][]int)
	}
	fields := make(map[string][]int)
	collectFields(t, nil, fields)
	fieldCache.Store(t, fields)
	return fields
}

func collectFields(t reflect.Type, prefix []int, into map[string][]int) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		index := append(append([]int(nil), prefix...), i)

		tag, _, _ := strings.Cut(f.Tag.Get("db"), ",")
		if tag == "-" {
			continue
		}

		if f.Anonymous && tag == "" {
			ft := f.Type
			if ft.Kind() == reflect.Ptr {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct && !reflect.PtrTo(ft).Implements(scannerType) {
				collectFields(ft, index, into)
				continue
			}
		}
		if !f.IsExported() {
			continue
		}

		name := tag
		if name == "" {
			name = strings.ToLower(f.Name)
		}
		// The shallowest field wins, as with Go's own embedding rules.
		if prev, ok := into[name]; ok && len(prev) <= len(index) {
			continue
		}
		into[name] = index
	}
}

// fieldByIndexReadOnly reads a field, reporting false when an embedded
// pointer on the way is nil.
func fieldByIndexReadOnly(v reflect.Value, index []int) (reflect.Value, bool) {
	for _, i := range index {
		for v.Kind() == reflect.Ptr {
			if v.IsNil() {
				return reflect.Value{}, false
			}
			v = v.Elem()
		}
		v = v.Field(i)
	}
	return v, true
}

// fieldByIndexAlloc returns a settable field, allocating embedded pointers.
func fieldByIndexAlloc(v reflect.Value, index []int) reflect.Value {
	for _, i := range index {
		for v.Kind() == reflect.Ptr {
			if v.IsNil() {
				v.Set(reflect.New(v.Type().Elem()))
			}
			v = v.Elem()
		}
		v = v.Field(i)
	}
	return v
}

// isScannable reports whether t is filled from a single column rather than
// one column per field.
func isScannable(t reflect.Type) bool {
	if reflect.PtrTo(t).Implements(scannerType) {
		return true
	}
	if t.Kind() != reflect.Struct {
		return true
	}
	// time.Time and friends: a struct with nothing to name a column by.
	return len(fieldsOf(t)) == 0
}

// scanAll reads every row into dest, a pointer to a slice of structs, scalars
// or pointers to either.
func scanAll(rows *sql.Rows, dest any) error {
	defer rows.Close()

	dv := reflect.ValueOf(dest)
	if dv.Kind() != reflect.Ptr || dv.IsNil() {
		return fmt.Errorf("pdodb: destination must be a non-nil pointer to a slice, got %T", dest)
	}
	slice := dv.Elem()
	if slice.Kind() != reflect.Slice {
		return fmt.Errorf("pdodb: destination must be a pointer to a slice, got %T", dest)
	}

	elem := slice.Type().Elem()
	base := elem
	if base.Kind() == reflect.Ptr {
		base = base.Elem()
	}

	cols, err := rows.Columns()
	if err != nil {
		return err
	}

	scannable := isScannable(base)
	var targets [][]int
	if scannable {
		if len(cols) > 1 {
			return fmt.Errorf("pdodb: scannable destination %s needs one column, got %d", base, len(cols))
		}
	} else {
		if targets, err = columnTargets(base, cols); err != nil {
			return err
		}
	}

	for rows.Next() {
		item := reflect.New(base)
		if err := scanRow(rows, item.Elem(), scannable, targets); err != nil {
			return err
		}
		if elem.Kind() == reflect.Ptr {
			slice.Set(reflect.Append(slice, item))
		} else {
			slice.Set(reflect.Append(slice, item.Elem()))
		}
	}
	return rows.Err()
}

// scanOne reads the first row into dest, a pointer to a struct or scalar.
// It returns sql.ErrNoRows when the result is empty.
func scanOne(rows *sql.Rows, dest any) error {
	defer rows.Close()

	dv := reflect.ValueOf(dest)
	if dv.Kind() != reflect.Ptr || dv.IsNil() {
		return fmt.Errorf("pdodb: destination must be a non-nil pointer, got %T", dest)
	}
	target := dv.Elem()

	cols, err := rows.Columns()
	if err != nil {
		return err
	}

	scannable := isScannable(target.Type())
	var targets [][]int
	if scannable {
		if len(cols) > 1 {
			return fmt.Errorf("pdodb: scannable destination %s needs one column, got %d", target.Type(), len(cols))
		}
	} else if targets, err = columnTargets(target.Type(), cols); err != nil {
		return err
	}

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return sql.ErrNoRows
	}
	if err := scanRow(rows, target, scannable, targets); err != nil {
		return err
	}
	return rows.Close()
}

func columnTargets(t reflect.Type, cols []string) ([][]int, error) {
	fields := fieldsOf(t)
	targets := make([][]int, len(cols))
	for i, col := range cols {
		idx, ok := fields[col]
		if !ok {
			return nil, fmt.Errorf("pdodb: missing destination name %s in %s", col, reflect.PtrTo(t))
		}
		targets[i] = idx
	}
	return targets, nil
}

func scanRow(rows *sql.Rows, v reflect.Value, scannable bool, targets [][]int) error {
	if scannable {
		return rows.Scan(v.Addr().Interface())
	}
	values := make([]any, len(targets))
	for i, idx := range targets {
		values[i] = fieldByIndexAlloc(v, idx).Addr().Interface()
	}
	return rows.Scan(values...)
}

// scanMap reads the current row into a map of column name to value.
func scanMap(rows *sql.Rows, cols []string) (map[string]any, error) {
	values := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range values {
		ptrs[i] = &values[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return nil, err
	}
	row := make(map[string]any, len(cols))
	for i, col := range cols {
		row[col] = values[i]
	}
	return row, nil
}
