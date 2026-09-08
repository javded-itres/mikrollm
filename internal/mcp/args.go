package mcp

import (
	"encoding/json"
	"strconv"
	"strings"
)

func strArg(args map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := args[k]; ok {
			s := stringify(v)
			if s != "" {
				return s
			}
		}
	}
	return ""
}

func stringify(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case json.Number:
		return strings.TrimSpace(t.String())
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strings.TrimSpace(strconv.FormatFloat(t, 'f', -1, 64))
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

func hasArg(args map[string]any, key string) bool {
	_, ok := args[key]
	return ok
}

func boolArg(args map[string]any, key string, def bool) bool {
	v, ok := args[key]
	if !ok || v == nil {
		return def
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		s := strings.ToLower(strings.TrimSpace(t))
		return s == "1" || s == "true" || s == "yes" || s == "on"
	case float64:
		return t != 0
	case json.Number:
		n, _ := t.Int64()
		return n != 0
	default:
		return def
	}
}

func intArg(args map[string]any, keys ...string) (int64, bool) {
	for _, key := range keys {
		v, ok := args[key]
		if !ok || v == nil {
			continue
		}
		switch t := v.(type) {
		case float64:
			return int64(t), true
		case int:
			return int64(t), true
		case int64:
			return t, true
		case json.Number:
			n, err := t.Int64()
			return n, err == nil
		case string:
			s := strings.TrimSpace(t)
			if s == "" {
				continue
			}
			n, err := strconv.ParseInt(s, 10, 64)
			if err == nil {
				return n, true
			}
		}
	}
	return 0, false
}

func strSlice(args map[string]any, key string) ([]string, bool) {
	v, ok := args[key]
	if !ok || v == nil {
		return nil, false
	}
	switch t := v.(type) {
	case []string:
		out := make([]string, 0, len(t))
		for _, s := range t {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		return out, true
	case []any:
		out := make([]string, 0, len(t))
		for _, x := range t {
			s := stringify(x)
			if s != "" {
				out = append(out, s)
			}
		}
		return out, true
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return []string{}, true
		}
		if strings.Contains(s, ",") {
			var out []string
			for _, p := range strings.Split(s, ",") {
				p = strings.TrimSpace(p)
				if p != "" {
					out = append(out, p)
				}
			}
			return out, true
		}
		return []string{s}, true
	default:
		return nil, false
	}
}

func intSlice(args map[string]any, key string) ([]int64, bool) {
	v, ok := args[key]
	if !ok || v == nil {
		return nil, false
	}
	add := func(out []int64, raw any) []int64 {
		switch t := raw.(type) {
		case float64:
			if id := int64(t); id > 0 {
				return append(out, id)
			}
		case int:
			if t > 0 {
				return append(out, int64(t))
			}
		case int64:
			if t > 0 {
				return append(out, t)
			}
		case json.Number:
			if n, err := t.Int64(); err == nil && n > 0 {
				return append(out, n)
			}
		case string:
			if n, err := strconv.ParseInt(strings.TrimSpace(t), 10, 64); err == nil && n > 0 {
				return append(out, n)
			}
		}
		return out
	}
	switch t := v.(type) {
	case []any:
		var out []int64
		for _, x := range t {
			out = add(out, x)
		}
		return out, true
	case []float64:
		var out []int64
		for _, x := range t {
			out = add(out, x)
		}
		return out, true
	default:
		if n, ok := intArg(args, key); ok && n > 0 {
			return []int64{n}, true
		}
		return nil, false
	}
}
