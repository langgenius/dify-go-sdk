package kernel

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// Object is a decoded JSON object, read leniently.
//
// Dify's answers drift: a count arrives as a string on one route
// (binding_count on a tag), a timestamp as a float on another, a field goes
// missing on an older server. Decoding straight into tagged structs turns each
// of those into a failed call. Reading through these accessors turns them into
// a zero value instead, and every typed result keeps the whole answer on Raw
// so nothing is lost either way.
type Object map[string]any

func (o Object) Has(key string) bool {
	v, ok := o[key]
	return ok && v != nil
}

func (o Object) Str(key string) string { return AsString(o[key]) }

func (o Object) Int(key string) int { return int(o.Int64(key)) }

func (o Object) Int64(key string) int64 {
	n, _ := asInt64(o[key])
	return n
}

// IntPtr distinguishes absent from zero, for fields where the difference is
// information — a timestamp that was never set is not the epoch.
func (o Object) IntPtr(key string) *int64 {
	n, ok := asInt64(o[key])
	if !ok {
		return nil
	}
	return &n
}

func (o Object) Float(key string) float64 {
	f, _ := AsFloat(o[key])
	return f
}

func (o Object) Bool(key string) bool {
	switch v := o[key].(type) {
	case bool:
		return v
	case string:
		b, _ := strconv.ParseBool(v)
		return b
	case json.Number:
		n, _ := v.Int64()
		return n != 0
	}
	return false
}

func (o Object) Obj(key string) Object {
	if m, ok := o[key].(map[string]any); ok {
		return Object(m)
	}
	if m, ok := o[key].(Object); ok {
		return m
	}
	return Object{}
}

func (o Object) List(key string) []any {
	if l, ok := o[key].([]any); ok {
		return l
	}
	return nil
}

// Objs is the objects in a list, skipping anything that is not one.
func (o Object) Objs(key string) []Object {
	var out []Object
	for _, item := range o.List(key) {
		if m, ok := item.(map[string]any); ok {
			out = append(out, Object(m))
		}
	}
	return out
}

func (o Object) Strs(key string) []string {
	var out []string
	for _, item := range o.List(key) {
		if s := AsString(item); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// Maps is objs with the plain map type, for fields handed to the caller as-is.
func (o Object) Maps(key string) []map[string]any {
	var out []map[string]any
	for _, item := range o.Objs(key) {
		out = append(out, map[string]any(item))
	}
	return out
}

func (o Object) Raw() map[string]any { return map[string]any(o) }

func AsString(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		return v
	case json.Number:
		return v.String()
	case bool:
		return strconv.FormatBool(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return fmt.Sprint(v)
}

func asInt64(v any) (int64, bool) {
	switch v := v.(type) {
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return n, true
		}
		if f, err := v.Float64(); err == nil {
			return int64(f), true
		}
	case float64:
		return int64(v), true
	case int:
		return int64(v), true
	case int64:
		return v, true
	case string:
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n, true
		}
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return int64(f), true
		}
	}
	return 0, false
}

func AsFloat(v any) (float64, bool) {
	switch v := v.(type) {
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case string:
		f, err := strconv.ParseFloat(v, 64)
		return f, err == nil
	}
	return 0, false
}

// OrEmpty keeps an inputs map from being sent as null, which Dify's payload
// models reject where they want an object.
func OrEmpty(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// Truthy mirrors how Dify's own code tests a value: a non-empty string is
// true even when it spells zero, a number is true when it is not zero.
func Truthy(v any) bool {
	switch v := v.(type) {
	case nil:
		return false
	case string:
		return v != ""
	case bool:
		return v
	case json.Number:
		f, err := v.Float64()
		return err != nil || f != 0
	case float64:
		return v != 0
	}
	return true
}
