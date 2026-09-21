// Package params applies per-model default parameters stored on a gateway
// alias. A profile lives on the alias; when a request goes through the proxy
// the profile fills in generation knobs the client did not set (or overrides
// them when the knob is locked).
package params

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/javded-itres/mikrollm/internal/domain"
)

// Profile keys are stored in Ollama-native naming; Inject translates them to
// whatever the target backend and request path understand.
var knownKeys = map[string]string{
	"temperature":       "number",
	"top_p":             "number",
	"top_k":             "integer",
	"min_p":             "number",
	"seed":              "integer",
	"repeat_penalty":    "number",
	"presence_penalty":  "number",
	"frequency_penalty": "number",
	"mirostat":          "integer",
	"mirostat_tau":      "number",
	"mirostat_eta":      "number",
	"think":             "think",
	"num_predict":       "integer",
	"num_ctx":           "integer",
	"max_tokens":        "integer",
	"stop":              "stop",
}

// Profile is the parsed params JSON of a model alias:
// {"think":"low","temperature":0.2,"locked":["think"],"x_custom":1}.
type Profile struct {
	Values map[string]any
	Locked map[string]bool
	Extra  map[string]any // x_* passthrough
}

func (p Profile) Empty() bool {
	return len(p.Values) == 0 && len(p.Extra) == 0
}

// ParseProfile validates and normalizes a params JSON document.
func ParseProfile(raw string) (Profile, error) {
	p := Profile{Values: map[string]any{}, Locked: map[string]bool{}, Extra: map[string]any{}}
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" {
		return p, nil
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return p, fmt.Errorf("params JSON: %w", err)
	}
	for k, v := range doc {
		switch k {
		case "locked":
			list, ok := v.([]any)
			if !ok {
				return p, fmt.Errorf("locked must be an array of keys")
			}
			for _, it := range list {
				key, _ := it.(string)
				if _, isKnown := knownKeys[key]; !isKnown && !strings.HasPrefix(key, "x_") {
					return p, fmt.Errorf("locked: unknown key %q", key)
				}
				p.Locked[key] = true
			}
		default:
			want, isKnown := knownKeys[k]
			switch {
			case strings.HasPrefix(k, "x_"):
				p.Extra[k] = v
			case !isKnown:
				return p, fmt.Errorf("unknown param %q (custom keys need an x_ prefix)", k)
			default:
				if err := checkType(k, want, v); err != nil {
					return p, err
				}
				p.Values[k] = v
			}
		}
	}
	for k := range p.Locked {
		if _, ok := p.Values[k]; !ok {
			if _, ok := p.Extra[k]; !ok {
				return p, fmt.Errorf("locked key %q has no value", k)
			}
		}
	}
	return p, nil
}

func checkType(key, want string, v any) error {
	switch want {
	case "number":
		if _, ok := v.(float64); !ok {
			return fmt.Errorf("%s must be a number", key)
		}
	case "integer":
		f, ok := v.(float64)
		if !ok || f != float64(int64(f)) {
			return fmt.Errorf("%s must be an integer", key)
		}
	case "think":
		switch t := v.(type) {
		case bool:
		case string:
			switch strings.ToLower(t) {
			case "low", "medium", "high", "max", "none":
			default:
				return fmt.Errorf("think must be true/false or low|medium|high|max|none, got %q", t)
			}
		default:
			return fmt.Errorf("think must be true/false or a level string")
		}
	case "stop":
		switch v.(type) {
		case string, []any:
		default:
			return fmt.Errorf("stop must be a string or an array of strings")
		}
	}
	return nil
}

// Inject merges the profile into an OpenAI or Ollama-native chat request body.
// Client-supplied fields win unless the key is locked. Bodies that are not a
// JSON object pass through untouched.
func Inject(body []byte, p Profile, kind string, native bool) []byte {
	if p.Empty() {
		return body
	}
	var raw map[string]any
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if dec.Decode(&raw) != nil {
		return body
	}
	if native {
		injectNative(raw, p)
	} else {
		injectOpenAI(raw, p, kind)
	}
	out, err := json.Marshal(raw)
	if err != nil {
		return body
	}
	return out
}

func has(raw map[string]any, key string, p Profile, pkey string) bool {
	if p.Locked[pkey] {
		return false // locked: always override
	}
	_, ok := raw[key]
	return ok
}

// injectOpenAI fills knobs for the /v1/chat/completions path.
func injectOpenAI(raw map[string]any, p Profile, kind string) {
	v := p.Values
	put := func(k string, val any) {
		raw[k] = val
	}
	if x, ok := v["temperature"]; ok && !has(raw, "temperature", p, "temperature") {
		put("temperature", x)
	}
	if x, ok := v["top_p"]; ok && !has(raw, "top_p", p, "top_p") {
		put("top_p", x)
	}
	if x, ok := v["max_tokens"]; ok && !has(raw, "max_tokens", p, "max_tokens") {
		put("max_tokens", x)
	} else if x, ok := v["num_predict"]; ok && !has(raw, "max_tokens", p, "num_predict") {
		put("max_tokens", x)
	}
	if x, ok := v["seed"]; ok && !has(raw, "seed", p, "seed") {
		put("seed", x)
	}
	if x, ok := v["stop"]; ok && !has(raw, "stop", p, "stop") {
		put("stop", x)
	}

	switch kind {
	case domain.KindOllama, domain.KindOllamaCloud:
		// Ollama's OpenAI-compat endpoint understands temperature/top_p/max_tokens
		// and reasoning effort; native-only knobs (num_ctx, penalties…) are skipped.
		if x, ok := v["think"]; ok {
			effort, enabled := thinkToEffort(x, kind)
			if p.Locked["think"] {
				if _, has := raw["reasoning_effort"]; has {
					delete(raw, "reasoning_effort")
				}
				deleteNestedThink(raw)
			}
			if _, ok := raw["reasoning_effort"]; !ok {
				if _, nested := raw["reasoning"]; !nested || p.Locked["think"] {
					if enabled {
						put("reasoning_effort", effort)
					} else {
						put("reasoning_effort", "none")
					}
				}
			}
		}
	case domain.KindOpenRouter:
		for _, k := range []string{"top_k", "min_p", "repeat_penalty", "presence_penalty", "frequency_penalty"} {
			if x, ok := v[k]; ok && !has(raw, k, p, k) {
				put(k, x)
			}
		}
		if x, ok := v["think"]; ok {
			applyOpenRouterReasoning(raw, p, x)
		}
	case domain.KindVLLM, domain.KindLMStudio:
		for _, k := range []string{"top_k", "min_p", "presence_penalty", "frequency_penalty"} {
			if x, ok := v[k]; ok && !has(raw, k, p, k) {
				put(k, x)
			}
		}
		if x, ok := v["repeat_penalty"]; ok && !has(raw, "repetition_penalty", p, "repeat_penalty") {
			put("repetition_penalty", x)
		}
		if x, ok := v["think"]; ok {
			applyTckThink(raw, p, x)
		}
	}
	for k, x := range p.Extra {
		if _, exists := raw[k]; !exists || p.Locked[k] {
			put(k, x)
		}
	}
}

// thinkToEffort maps the unified think knob to an effort level.
// false → disabled; true → high; level strings pass through.
func thinkToEffort(v any, kind string) (effort string, enabled bool) {
	switch t := v.(type) {
	case bool:
		if !t {
			return "", false
		}
		return "high", true
	case string:
		lv := strings.ToLower(t)
		if lv == "none" {
			return "", false
		}
		if kind == domain.KindOpenRouter && lv == "max" {
			return "high", true // OpenRouter knows only low/medium/high
		}
		return lv, true
	}
	return "", false
}

func deleteNestedThink(raw map[string]any) {
	if r, ok := raw["reasoning"].(map[string]any); ok {
		delete(r, "effort")
		delete(r, "enabled")
		if len(r) == 0 {
			delete(raw, "reasoning")
		}
	}
}

func applyOpenRouterReasoning(raw map[string]any, p Profile, v any) {
	if p.Locked["think"] {
		deleteNestedThink(raw)
		delete(raw, "reasoning_effort")
	} else if _, exists := raw["reasoning"]; exists {
		return
	} else if _, exists := raw["reasoning_effort"]; exists {
		return
	}
	effort, enabled := thinkToEffort(v, domain.KindOpenRouter)
	if !enabled {
		raw["reasoning"] = map[string]any{"enabled": false}
		return
	}
	raw["reasoning"] = map[string]any{"effort": effort}
}

// applyTckThink maps think to chat_template_kwargs for vLLM / LM Studio.
func applyTckThink(raw map[string]any, p Profile, v any) {
	tck, _ := raw["chat_template_kwargs"].(map[string]any)
	if tck == nil {
		tck = map[string]any{}
	}
	if !p.Locked["think"] {
		if _, ok := tck["enable_thinking"]; ok {
			return
		}
		if _, ok := tck["reasoning_effort"]; ok {
			return
		}
	}
	switch t := v.(type) {
	case bool:
		tck["enable_thinking"] = t
	case string:
		if strings.ToLower(t) == "none" {
			tck["enable_thinking"] = false
		} else {
			tck["reasoning_effort"] = strings.ToLower(t)
		}
	}
	if len(tck) > 0 {
		raw["chat_template_kwargs"] = tck
	}
}

// injectNative fills knobs for the Ollama-native /api/chat path:
// sampling params go under options, think is top-level.
var nativeOptions = map[string]string{
	"temperature":       "temperature",
	"top_p":             "top_p",
	"top_k":             "top_k",
	"min_p":             "min_p",
	"seed":              "seed",
	"repeat_penalty":    "repeat_penalty",
	"presence_penalty":  "presence_penalty",
	"frequency_penalty": "frequency_penalty",
	"mirostat":          "mirostat",
	"mirostat_tau":      "mirostat_tau",
	"mirostat_eta":      "mirostat_eta",
	"num_predict":       "num_predict",
	"num_ctx":           "num_ctx",
	"max_tokens":        "num_predict",
	"stop":              "stop",
}

func injectNative(raw map[string]any, p Profile) {
	opts, _ := raw["options"].(map[string]any)
	if opts == nil {
		opts = map[string]any{}
	}
	changed := false
	for pk, ok := range nativeOptions {
		val, exists := p.Values[pk]
		if !exists {
			continue
		}
		if _, clientSet := opts[ok]; !clientSet || p.Locked[pk] {
			opts[ok] = val
			changed = true
		}
	}
	if changed {
		raw["options"] = opts
	}
	if x, ok := p.Values["think"]; ok {
		if _, clientSet := raw["think"]; !clientSet || p.Locked["think"] {
			raw["think"] = x
		}
	}
	for k, x := range p.Extra {
		if _, clientSet := raw[k]; !clientSet || p.Locked[k] {
			raw[k] = x
		}
	}
}
