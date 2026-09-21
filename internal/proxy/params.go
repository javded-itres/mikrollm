package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strings"
	"time"

	"github.com/javded-itres/mikrollm/internal/domain"
	"github.com/javded-itres/mikrollm/internal/params"
)

func (p *Proxy) ServeModelParams(w http.ResponseWriter, r *http.Request) {
	model := strings.TrimSpace(r.URL.Query().Get("model"))
	if model == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "model required"}})
		return
	}
	writeJSON(w, http.StatusOK, p.modelParams(model))
}

func (p *Proxy) modelParams(alias string) map[string]any {
	media := p.exclusiveMedia(alias)
	modality := media
	if modality == "" {
		modality = domain.MediaChat
	}
	kind := ""
	upstream := alias
	var b domain.Backend
	if got, up, err := p.pick(alias); err == nil {
		b = got
		kind = b.KindNorm()
		if up != "" {
			upstream = up
		}
	}
	out := map[string]any{
		"model":               alias,
		"upstream":            upstream,
		"kind":                kind,
		"modality":            modality,
		"required_parameters": []string{},
		"parameters":          defaultModelParams(modality),
	}
	if m, err := p.st.GetModelByAlias(alias); err == nil {
		applyProfileToPanel(out, m.Params)
	}
	if kind != domain.KindOpenComfy || strings.TrimSpace(b.BaseURL) == "" {
		return out
	}
	fetched, err := p.fetchOpenComfyModel(b, upstream)
	if err != nil {
		out["warning"] = err.Error()
		return out
	}
	return mergeOpenComfyParams(out, fetched, modality)
}

func (p *Proxy) fetchOpenComfyModel(b domain.Backend, id string) (map[string]any, error) {
	u := strings.TrimRight(b.BaseURL, "/") + "/v1/models/" + neturl.PathEscape(id)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	domain.ApplyUpstreamHeaders(req.Header, b)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	req = req.WithContext(ctx)
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("opencomfy %s", resp.Status)
	}
	var m map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

func mergeOpenComfyParams(out, fetched map[string]any, modality string) map[string]any {
	if fetched == nil {
		return out
	}
	if v, ok := fetched["parameters"]; ok {
		out["parameters"] = v
	}
	if v, ok := fetched["required_parameters"]; ok {
		out["required_parameters"] = v
	}
	if v, ok := fetched["input_schema"]; ok {
		out["input_schema"] = v
	}
	if v, ok := fetched["mcp_tool"]; ok {
		out["mcp_tool"] = v
	}
	if v, _ := fetched["name"].(string); v != "" {
		out["name"] = v
	}
	if arch, ok := fetched["architecture"].(map[string]any); ok {
		if mods, ok := arch["output_modalities"].([]any); ok {
			for _, m := range mods {
				s, _ := m.(string)
				if s == domain.MediaImage || s == domain.MediaVideo {
					out["modality"] = s
					modality = s
				}
			}
		}
	}
	params, _ := asParamMaps(out["parameters"])
	req, _ := asStringSlice(out["required_parameters"])
	params, req = ensureMediaKnobs(params, modality, req)
	out["parameters"] = params
	out["required_parameters"] = req
	return out
}

func asParamMaps(v any) ([]map[string]any, bool) {
	switch t := v.(type) {
	case []map[string]any:
		return t, true
	case []any:
		var out []map[string]any
		for _, it := range t {
			if m, ok := it.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out, true
	default:
		return nil, false
	}
}

func asStringSlice(v any) ([]string, bool) {
	switch t := v.(type) {
	case []string:
		return t, true
	case []any:
		var out []string
		for _, it := range t {
			if s, ok := it.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out, true
	default:
		return nil, false
	}
}

func ensureMediaKnobs(params []map[string]any, modality string, required []string) ([]map[string]any, []string) {
	has := map[string]bool{}
	for _, p := range params {
		n, _ := p["name"].(string)
		if n != "" {
			has[n] = true
		}
	}
	add := func(p map[string]any) {
		n, _ := p["name"].(string)
		if n == "" || has[n] {
			return
		}
		params = append(params, p)
		has[n] = true
	}
	if modality == domain.MediaImage {
		add(map[string]any{"name": "size", "type": "string", "default": "1024x1024"})
	}
	if modality == domain.MediaVideo {
		add(map[string]any{"name": "seconds", "type": "integer", "default": 4})
		add(map[string]any{"name": "size", "type": "string", "default": "720x1280"})
	}
	if required == nil {
		required = []string{}
	}
	return params, required
}

// applyProfileToPanel surfaces the alias params profile in the playground panel:
// profile values become the advertised defaults, think appears as a knob.
func applyProfileToPanel(out map[string]any, rawParams string) {
	if strings.TrimSpace(rawParams) == "" {
		return
	}
	pr, err := params.ParseProfile(rawParams)
	if err != nil || pr.Empty() {
		return
	}
	plist, _ := asParamMaps(out["parameters"])
	num := func(key string) (float64, bool) {
		v, ok := pr.Values[key]
		if !ok {
			return 0, false
		}
		f, ok := v.(float64)
		return f, ok
	}
	for _, pm := range plist {
		switch pm["name"] {
		case "temperature":
			if f, ok := num("temperature"); ok {
				pm["default"] = f
			}
		case "max_tokens":
			if f, ok := num("num_predict"); ok {
				pm["default"] = f
			} else if f, ok := num("max_tokens"); ok {
				pm["default"] = f
			}
		}
	}
	if v, ok := pr.Values["think"]; ok {
		knob := map[string]any{"name": "think", "type": "string", "default": "inherit",
			"options": []string{"inherit", "true", "false", "low", "medium", "high", "max"}}
		switch t := v.(type) {
		case bool:
			knob["default"] = fmt.Sprintf("%t", t)
		case string:
			knob["default"] = t
		}
		has := false
		for _, pm := range plist {
			if pm["name"] == "think" {
				has = true
			}
		}
		if !has {
			plist = append(plist, knob)
		}
	}
	out["parameters"] = plist
}

func defaultModelParams(modality string) []map[string]any {
	switch modality {
	case domain.MediaImage:
		return []map[string]any{
			{"name": "size", "type": "string", "default": "1024x1024"},
			{"name": "input_image", "type": "image", "required": false},
		}
	case domain.MediaVideo:
		return []map[string]any{
			{"name": "seconds", "type": "integer", "default": 4, "min": 1, "max": 60},
			{"name": "size", "type": "string", "default": "720x1280"},
			{"name": "input_image", "type": "image", "required": false},
		}
	default:
		return []map[string]any{
			{"name": "temperature", "type": "number", "default": 0.7, "min": 0, "max": 2},
			{"name": "max_tokens", "type": "integer", "default": 1024, "min": 16, "max": 8192},
		}
	}
}
