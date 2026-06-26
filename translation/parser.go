package translation

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
)

// fieldSchema is the per-type description of one translatable leaf field. It is
// independent of any instance, so it can be cached by reflect.Type.
type fieldSchema struct {
	path       []string // field path from the root struct, e.g. ["Label", "Title"]
	fieldName  string   // dotted path, e.g. "Label.Title"
	columnName string
	isPrimary  bool
}

// schemaCache memoizes the (reflection-heavy) tag walk per concrete type.
var schemaCache sync.Map // reflect.Type -> schemaEntry

type schemaEntry struct {
	fields []fieldSchema
	err    error
}

// parseTranslateTags returns the translatable fields of model with their current
// values. The structural walk is cached per type; only value extraction runs per
// call.
func parseTranslateTags(model Translatable) ([]fieldInfo, error) {
	val := reflect.ValueOf(model)
	if val.Kind() == reflect.Pointer {
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct {
		return nil, fmt.Errorf("model must be a struct, got %s", val.Kind())
	}

	schema, err := schemaForType(val.Type())
	if err != nil {
		return nil, err
	}

	fields := make([]fieldInfo, 0, len(schema))
	for _, fs := range schema {
		fv, ok := valueByPath(val, fs.path)
		// A nested field whose parent struct is absent/empty contributes nothing;
		// top-level fields are always emitted (possibly with an empty value).
		if !ok && len(fs.path) > 1 {
			continue
		}
		info := fieldInfo{
			fieldName:  fs.fieldName,
			columnName: fs.columnName,
			isPrimary:  fs.isPrimary,
		}
		if ok && fv.CanInterface() {
			info.value = fmt.Sprintf("%v", fv.Interface())
		}
		fields = append(fields, info)
	}
	return fields, nil
}

// schemaForType returns the cached translatable-field schema for t.
func schemaForType(t reflect.Type) ([]fieldSchema, error) {
	if cached, ok := schemaCache.Load(t); ok {
		e := cached.(schemaEntry)
		return e.fields, e.err
	}

	var fields []fieldSchema
	var primaryFound bool
	err := walkTags(t, nil, &fields, &primaryFound, map[reflect.Type]bool{})
	if err == nil && !primaryFound {
		err = ErrNoPrimaryKey
	}

	schemaCache.Store(t, schemaEntry{fields: fields, err: err})
	return fields, err
}

// walkTags discovers translatable leaf fields on t, recursing into untagged
// nested structs. visited guards against recursive type definitions.
func walkTags(t reflect.Type, prefix []string, fields *[]fieldSchema, primaryFound *bool, visited map[reflect.Type]bool) error {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct || visited[t] {
		return nil
	}
	visited[t] = true
	defer delete(visited, t)

	for field := range t.Fields() {
		tag := field.Tag.Get("translate")

		ft := field.Type
		if ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}

		if tag == "" {
			// Recurse into untagged nested structs to reach their tagged fields.
			if ft.Kind() == reflect.Struct {
				if err := walkTags(ft, append(prefix, field.Name), fields, primaryFound, visited); err != nil {
					return err
				}
			}
			continue
		}

		path := append(append([]string{}, prefix...), field.Name)
		fs := fieldSchema{path: path, fieldName: strings.Join(path, ".")}

		if tag == "primary" {
			if *primaryFound {
				return ErrMultiplePrimaryKeys
			}
			fs.isPrimary = true
			fs.columnName = "primary"
			*primaryFound = true
		} else {
			// Translatable (non-primary) fields must be strings: apply only sets
			// strings, so accepting other kinds would silently fail later.
			if ft.Kind() != reflect.String {
				return fmt.Errorf("%w: field %s must be a string, got %s", ErrInvalidTag, fs.fieldName, ft.Kind())
			}
			fs.columnName = tag
		}

		*fields = append(*fields, fs)
	}
	return nil
}

// valueByPath navigates a struct value along path, dereferencing pointers and
// stopping (ok=false) at a nil pointer, a missing field, or a zero-valued
// intermediate nested struct (so empty nested models contribute no fields).
func valueByPath(root reflect.Value, path []string) (reflect.Value, bool) {
	cur := root
	for i, name := range path {
		if cur.Kind() == reflect.Pointer {
			if cur.IsNil() {
				return reflect.Value{}, false
			}
			cur = cur.Elem()
		}
		if cur.Kind() != reflect.Struct {
			return reflect.Value{}, false
		}
		// An empty nested struct on the way down means the leaf is absent.
		if i > 0 && cur.IsZero() {
			return reflect.Value{}, false
		}
		cur = cur.FieldByName(name)
		if !cur.IsValid() {
			return reflect.Value{}, false
		}
	}
	return cur, true
}

// setFieldValue sets a string field by dotted path using reflection.
func setFieldValue(model Translatable, fieldName, value string) error {
	val := reflect.ValueOf(model)
	if val.Kind() == reflect.Pointer {
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct {
		return fmt.Errorf("model must be a struct, got %s", val.Kind())
	}

	field, ok := valueByPath(val, strings.Split(fieldName, "."))
	if !ok {
		return fmt.Errorf("field %s not found", fieldName)
	}
	if !field.CanSet() {
		return fmt.Errorf("field %s cannot be set", fieldName)
	}
	if field.Kind() != reflect.String {
		return fmt.Errorf("field %s must be a string, got %s", fieldName, field.Kind())
	}
	field.SetString(value)
	return nil
}

// translatableType is the reflected Translatable interface, computed once.
var translatableType = reflect.TypeFor[Translatable]()

// collectNestedTranslatables recursively collects Translatable values reachable
// through slice fields. visited (keyed by pointer) guards against cycles.
func collectNestedTranslatables(model Translatable) []Translatable {
	return collectNested(model, map[uintptr]bool{})
}

func collectNested(model Translatable, visited map[uintptr]bool) []Translatable {
	val := reflect.ValueOf(model)
	if val.Kind() == reflect.Pointer {
		if val.IsNil() {
			return nil
		}
		ptr := val.Pointer()
		if visited[ptr] {
			return nil
		}
		visited[ptr] = true
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct {
		return nil
	}

	var result []Translatable
	typ := val.Type()
	for i := range val.NumField() {
		field := typ.Field(i)
		if field.Type.Kind() != reflect.Slice {
			continue
		}

		elemType := field.Type.Elem()
		check := elemType
		if elemType.Kind() != reflect.Pointer {
			check = reflect.PointerTo(elemType)
		}
		if !check.Implements(translatableType) {
			continue
		}

		fieldVal := val.Field(i)
		for j := 0; j < fieldVal.Len(); j++ {
			elem := fieldVal.Index(j)
			if elem.Kind() == reflect.Pointer && elem.IsNil() {
				continue
			}
			if !elem.CanInterface() {
				continue
			}
			tr, ok := elem.Interface().(Translatable)
			if !ok {
				continue
			}
			result = append(result, tr)
			result = append(result, collectNested(tr, visited)...)
		}
	}
	return result
}
