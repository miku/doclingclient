package doclingclient

import (
	"encoding/json"
	"fmt"
	"mime/multipart"
	"reflect"
	"strings"
)

// encodeFormFields writes the exported fields of struct v to mw as
// multipart form fields, keyed by their json: tag name with prefix prepended.
// Fields tagged `json:"-"` are skipped. omitempty/omitzero are honored.
//
// Scalars are written as fmt.Fprint of their value. Slices emit one form
// field per element. Maps and nested structs are JSON-marshaled into a single
// form field — useful for complex option blocks like PictureDescriptionAPI.
//
// v may be a struct or a pointer to one. A nil pointer is a no-op.
func encodeFormFields(mw *multipart.Writer, v any, prefix string) error {
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return fmt.Errorf("doclingclient: encodeFormFields: want struct, got %s", rv.Kind())
	}
	t := rv.Type()
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if !sf.IsExported() {
			continue
		}
		tag := sf.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, opts := splitJSONTag(tag)
		if name == "" {
			name = strings.ToLower(sf.Name)
		}
		f := rv.Field(i)
		if (hasOpt(opts, "omitempty") && isEmptyValue(f)) || (hasOpt(opts, "omitzero") && f.IsZero()) {
			continue
		}
		if err := writeFormValue(mw, prefix+name, f); err != nil {
			return err
		}
	}
	return nil
}

func writeFormValue(mw *multipart.Writer, name string, v reflect.Value) error {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return nil
		}
		return writeFormValue(mw, name, v.Elem())
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64,
		reflect.String:
		return mw.WriteField(name, fmt.Sprint(v.Interface()))
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if err := writeFormValue(mw, name, v.Index(i)); err != nil {
				return err
			}
		}
		return nil
	case reflect.Map, reflect.Struct:
		b, err := json.Marshal(v.Interface())
		if err != nil {
			return err
		}
		return mw.WriteField(name, string(b))
	default:
		return fmt.Errorf("doclingclient: encodeFormFields: unsupported field kind %s for %q", v.Kind(), name)
	}
}

func splitJSONTag(tag string) (name, opts string) {
	name, opts, _ = strings.Cut(tag, ",")
	return name, opts
}

func hasOpt(opts, want string) bool {
	for opts != "" {
		var part string
		part, opts, _ = strings.Cut(opts, ",")
		if part == want {
			return true
		}
	}
	return false
}

func isEmptyValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64,
		reflect.Interface, reflect.Pointer:
		return v.IsZero()
	}
	return false
}
