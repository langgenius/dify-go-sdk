package dify

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// object is a decoded JSON object, read leniently.
//
// Dify's answers drift: a count arrives as a string on one route
// (binding_count on a tag), a timestamp as a float on another, a field goes
// missing on an older server. Decoding straight into tagged structs turns each
// of those into a failed call. Reading through these accessors turns them into
// a zero value instead, and every typed result keeps the whole answer on Raw
// so nothing is lost either way.
type object map[string]any

func (o object) has(key string) bool {
	v, ok := o[key]
	return ok && v != nil
}

func (o object) str(key string) string { return asString(o[key]) }

func (o object) int(key string) int { return int(o.int64(key)) }

func (o object) int64(key string) int64 {
	n, _ := asInt64(o[key])
	return n
}

// intPtr distinguishes absent from zero, for fields where the difference is
// information — a timestamp that was never set is not the epoch.
func (o object) intPtr(key string) *int64 {
	n, ok := asInt64(o[key])
	if !ok {
		return nil
	}
	return &n
}

func (o object) float(key string) float64 {
	f, _ := asFloat(o[key])
	return f
}

func (o object) bool(key string) bool {
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

func (o object) obj(key string) object {
	if m, ok := o[key].(map[string]any); ok {
		return object(m)
	}
	if m, ok := o[key].(object); ok {
		return m
	}
	return object{}
}

func (o object) list(key string) []any {
	if l, ok := o[key].([]any); ok {
		return l
	}
	return nil
}

// objs is the objects in a list, skipping anything that is not one.
func (o object) objs(key string) []object {
	var out []object
	for _, item := range o.list(key) {
		if m, ok := item.(map[string]any); ok {
			out = append(out, object(m))
		}
	}
	return out
}

func (o object) strs(key string) []string {
	var out []string
	for _, item := range o.list(key) {
		if s := asString(item); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// maps is objs with the plain map type, for fields handed to the caller as-is.
func (o object) maps(key string) []map[string]any {
	var out []map[string]any
	for _, item := range o.objs(key) {
		out = append(out, map[string]any(item))
	}
	return out
}

func (o object) raw() map[string]any { return map[string]any(o) }

func asString(v any) string {
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

func asFloat(v any) (float64, bool) {
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

// orEmpty keeps an inputs map from being sent as null, which Dify's payload
// models reject where they want an object.
func orEmpty(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// truthy mirrors how Dify's own code tests a value: a non-empty string is
// true even when it spells zero, a number is true when it is not zero.
func truthy(v any) bool {
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
