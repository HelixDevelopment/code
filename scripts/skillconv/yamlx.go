package skillconv

import (
	"fmt"
	"strconv"
	"strings"
)

// scalarToString coerces a YAML scalar (or simple composite) to its string
// form. Used for dialect slots the production types declare as string even
// when the wild corpus occasionally carries another scalar shape (e.g.
// `version: 1` unquoted, GAP-03). Every coercion is content-preserving for
// the values observed in-tree; composites join (lists) rather than error, and
// the normalisation is named in evidence, never silent.
func scalarToString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case uint64:
		return strconv.FormatUint(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case []any:
		parts := make([]string, 0, len(t))
		for _, e := range t {
			if s := scalarToString(e); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ", ")
	default:
		return fmt.Sprintf("%v", v)
	}
}

// coerceStringList reads a YAML sequence (or single scalar) as []string.
func coerceStringList(v any) []string {
	switch t := v.(type) {
	case nil:
		return nil
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			out = append(out, scalarToString(e))
		}
		return out
	case string:
		if t == "" {
			return nil
		}
		return []string{t}
	default:
		return []string{scalarToString(v)}
	}
}

// coerceStringMap reads a YAML mapping as map[string]string, coercing scalar
// values. Non-map input yields an empty map.
func coerceStringMap(v any) map[string]string {
	out := map[string]string{}
	if m, ok := v.(map[string]any); ok {
		for k, val := range m {
			out[k] = scalarToString(val)
		}
	}
	return out
}
