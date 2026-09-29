package settings

import (
	"encoding/json"
	"strconv"
	"strings"
)

// TypedValue converts a raw value into the JSON shape a configuration file
// should hold for it.
//
// Everything inside the resolver is a string, because a string is what a
// person types into an environment variable and putting a second
// representation next to it is how the two drift. A file is different: a
// reader opening it expects `"max": 8` and not `"max": "8"`, and a hand-edit
// that produced the number should not be rewritten into a string by the next
// `mcpx settings set`.
func TypedValue(set Setting, raw string) any {
	raw = strings.TrimSpace(raw)
	switch set.Kind {
	case KindBool:
		if b, err := strconv.ParseBool(raw); err == nil {
			return b
		}
	case KindInt:
		if n, err := strconv.Atoi(raw); err == nil {
			return n
		}
	case KindList, KindPathList:
		if raw == "" {
			return []any{}
		}
		if strings.HasPrefix(raw, "[") {
			var arr []any
			if json.Unmarshal([]byte(raw), &arr) == nil {
				return arr
			}
		}
		parts := splitList(raw)
		arr := make([]any, 0, len(parts))
		for _, p := range parts {
			if p == NullMarker {
				arr = append(arr, nil)
				continue
			}
			arr = append(arr, p)
		}
		return arr
	}
	return raw
}
