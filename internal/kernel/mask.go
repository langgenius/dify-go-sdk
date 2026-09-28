package kernel

import "strings"

// MaskSecret renders a key the way it is safe to print: app-****3f2a.
//
// Dify issues keys with a type prefix (app-, dataset-), which is kept so a
// masked key is still identifiable.
func MaskSecret(value string) string {
	const keep = 4
	if value == "" {
		return "****"
	}
	head, tail := "", value
	if prefix, rest, ok := strings.Cut(value, "-"); ok && len(prefix) <= 8 {
		head, tail = prefix+"-", rest
	}
	// Showing the tail of a short value would reveal most of it.
	if len(tail) <= keep*2 {
		return head + "****"
	}
	return head + "****" + tail[len(tail)-keep:]
}
