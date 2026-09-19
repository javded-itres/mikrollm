package promptcache

import "encoding/json"

type Usage struct {
	PromptTokens     int
	CompletionTokens int
	CachedTokens     int
	CacheWriteTokens int
	CacheDiscount    float64
	HasDiscount      bool
	Cost             float64
	HasCost          bool
}

func ParseJSON(event []byte) Usage {
	if len(event) == 0 {
		return Usage{}
	}
	var raw map[string]any
	if json.Unmarshal(event, &raw) != nil || raw == nil {
		return Usage{}
	}
	var u Usage
	if um, ok := raw["usage"].(map[string]any); ok {
		u = ParseUsageMap(um)
	} else {
		u.PromptTokens = asToken(raw["prompt_eval_count"])
		u.CompletionTokens = asToken(raw["eval_count"])
	}
	if !u.HasDiscount {
		if v, ok := raw["cache_discount"]; ok {
			u.HasDiscount = true
			u.CacheDiscount = toFloat(v)
		}
	}
	return u
}

func ParseUsageMap(u map[string]any) Usage {
	if u == nil {
		return Usage{}
	}
	out := Usage{
		PromptTokens:     maxToken(u["prompt_tokens"], u["input_tokens"]),
		CompletionTokens: maxToken(u["completion_tokens"], u["output_tokens"]),
	}
	var details map[string]any
	if d, ok := u["prompt_tokens_details"].(map[string]any); ok {
		details = d
	} else if d, ok := u["input_tokens_details"].(map[string]any); ok {
		details = d
	}
	cached, write := 0, 0
	if details != nil {
		cached = asToken(details["cached_tokens"])
		write = asToken(details["cache_write_tokens"])
	}
	out.CachedTokens = maxInt(cached, asToken(u["cache_read_input_tokens"]))
	out.CacheWriteTokens = maxInt(write, asToken(u["cache_creation_input_tokens"]))
	if v, ok := u["cache_discount"]; ok {
		out.HasDiscount = true
		out.CacheDiscount = toFloat(v)
	}
	if v, ok := u["cost"]; ok {
		out.HasCost = true
		out.Cost = toFloat(v)
	}
	return out
}

func maxToken(a, b any) int {
	return maxInt(asToken(a), asToken(b))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func asToken(v any) int {
	n := int(toFloat(v))
	if n < 0 {
		return 0
	}
	return n
}

func toFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case json.Number:
		f, err := t.Float64()
		if err != nil {
			return 0
		}
		return f
	case string:
		var n json.Number = json.Number(t)
		f, err := n.Float64()
		if err != nil {
			return 0
		}
		return f
	default:
		return 0
	}
}
