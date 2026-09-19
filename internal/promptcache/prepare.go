package promptcache

import (
	"encoding/json"
	"strings"

	"github.com/javded-itres/mikrollm/internal/domain"
)

type PrepInput struct {
	Body      []byte
	SendAs    string
	OrigModel string
	Kind      string
	Provider  string
	Mode      string
}

func Prepare(in PrepInput) ([]byte, bool) {
	if in.Kind != domain.KindOpenRouter {
		return rewriteModel(in.Body, in.SendAs)
	}
	raw, ok := decodeMap(in.Body)
	if !ok {
		return in.Body, false
	}
	changed := false
	if in.SendAs != "" {
		if cur, _ := raw["model"].(string); cur != in.SendAs {
			raw["model"] = in.SendAs
			changed = true
		}
	}
	if ensureIncludeUsage(raw) {
		changed = true
	}
	if !HasCacheHints(raw) {
		switch in.Mode {
		case ModeOn:
			raw["cache_control"] = map[string]any{"type": "ephemeral"}
			changed = true
		case ModeAuto:
			orig := in.OrigModel
			if orig == "" {
				orig, _ = raw["model"].(string)
			}
			if NeedsAnthropicTopLevel(in.Provider, in.SendAs, orig) {
				raw["cache_control"] = map[string]any{"type": "ephemeral"}
				changed = true
			}
		}
	}
	if !changed {
		return in.Body, false
	}
	out, err := json.Marshal(raw)
	if err != nil {
		return in.Body, false
	}
	return out, true
}

func rewriteModel(body []byte, sendAs string) ([]byte, bool) {
	if sendAs == "" {
		return body, false
	}
	raw, ok := decodeMap(body)
	if !ok {
		return body, false
	}
	if cur, _ := raw["model"].(string); cur == sendAs {
		return body, false
	}
	raw["model"] = sendAs
	out, err := json.Marshal(raw)
	if err != nil {
		return body, false
	}
	return out, true
}

func decodeMap(body []byte) (map[string]any, bool) {
	var raw map[string]any
	if json.Unmarshal(body, &raw) != nil || raw == nil {
		return nil, false
	}
	return raw, true
}

func HasCacheHints(raw map[string]any) bool {
	if raw == nil {
		return false
	}
	if _, ok := raw["cache_control"]; ok {
		return true
	}
	if _, ok := raw["prompt_cache_options"]; ok {
		return true
	}
	return nestedHint(raw)
}

func nestedHint(v any) bool {
	switch t := v.(type) {
	case map[string]any:
		if _, ok := t["cache_control"]; ok {
			return true
		}
		if _, ok := t["prompt_cache_breakpoint"]; ok {
			return true
		}
		for _, x := range t {
			if nestedHint(x) {
				return true
			}
		}
	case []any:
		for _, x := range t {
			if nestedHint(x) {
				return true
			}
		}
	}
	return false
}

func NeedsAnthropicTopLevel(provider, sendAs, origModel string) bool {
	if strings.EqualFold(strings.TrimSpace(provider), "Anthropic") {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(domain.ProviderLabel(provider)), "Anthropic") {
		return true
	}
	return hasAnthropicPrefix(sendAs) || hasAnthropicPrefix(origModel)
}

func hasAnthropicPrefix(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(s, "anthropic/")
}

func ensureIncludeUsage(raw map[string]any) bool {
	if !isStream(raw) {
		return false
	}
	so, ok := raw["stream_options"].(map[string]any)
	if !ok {
		if _, exists := raw["stream_options"]; exists {
			return false
		}
		raw["stream_options"] = map[string]any{"include_usage": true}
		return true
	}
	if _, exists := so["include_usage"]; exists {
		return false
	}
	so["include_usage"] = true
	raw["stream_options"] = so
	return true
}

func isStream(raw map[string]any) bool {
	switch t := raw["stream"].(type) {
	case bool:
		return t
	case string:
		return t == "true" || t == "1"
	default:
		return false
	}
}
