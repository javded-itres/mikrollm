package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/javded-itres/mikrollm/internal/auth"
	"github.com/javded-itres/mikrollm/internal/domain"
)

// SystemOne is POST /v1/systemone, the Jev decision API.
// Local Ollama 0.35 (nimble, tev1) and a hosted Jev base URL use the same body.
func (p *Proxy) SystemOne(w http.ResponseWriter, r *http.Request) {
	k, ok := p.requireKey(w, r)
	if !ok {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "read body"}})
		return
	}
	var req struct {
		Model     string          `json:"model"`
		State     json.RawMessage `json:"state"`
		Questions json.RawMessage `json:"questions"`
	}
	if json.Unmarshal(body, &req) != nil || strings.TrimSpace(req.Model) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "model is required"}})
		return
	}
	if !presentState(req.State) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "state is required"}})
		return
	}
	if !nonemptyObject(req.Questions) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "questions is required"}})
		return
	}
	if k.ID != 0 {
		p.st.TouchKey(k.ID)
	}
	if !auth.ModelAllowed(k, req.Model) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": map[string]any{"message": "model not allowed for this key"}})
		return
	}
	b, upstream, err := p.pick(req.Model)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": map[string]any{"message": err.Error()}})
		return
	}
	if b.KindNorm() == domain.KindHub {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "решения Jev через hub пока не проксируются"}})
		return
	}
	payload := rewriteModel(body, upstream)
	start := time.Now()
	p.health.Inc(b.ID)
	defer p.health.Dec(b.ID)
	httpReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, strings.TrimRight(b.BaseURL, "/")+b.SystemOnePath(), bytes.NewReader(payload))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": map[string]any{"message": err.Error()}})
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	domain.ApplyUpstreamHeaders(httpReq.Header, b)
	resp, err := p.client.Do(httpReq)
	if err != nil {
		p.st.Log(k.Prefix, req.Model, b.Name, 502, time.Since(start), 0, domain.TokenUsage{})
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": map[string]any{"message": err.Error()}})
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 400 {
		p.st.Log(k.Prefix, req.Model, b.Name, resp.StatusCode, time.Since(start), int64(len(respBody)), domain.TokenUsage{})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(respBody)
		return
	}
	usage := decisionUsage(respBody)
	out := rewriteResponseModel(respBody, req.Model)
	p.st.Log(k.Prefix, req.Model, b.Name, resp.StatusCode, time.Since(start), int64(len(out)), usage)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

func presentState(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return false
	}
	if raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) != nil || strings.TrimSpace(s) == "" {
			return false
		}
	}
	return true
}

func nonemptyObject(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		return false
	}
	var m map[string]json.RawMessage
	return json.Unmarshal(raw, &m) == nil && len(m) > 0
}

func decisionUsage(body []byte) domain.TokenUsage {
	var raw struct {
		Usage struct {
			Input  int `json:"input_tokens"`
			Output int `json:"output_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(body, &raw) != nil {
		return domain.TokenUsage{}
	}
	return domain.TokenUsage{PromptTokens: raw.Usage.Input, CompletionTokens: raw.Usage.Output}
}

func rewriteResponseModel(body []byte, alias string) []byte {
	if alias == "" {
		return body
	}
	var raw map[string]any
	if json.Unmarshal(body, &raw) != nil {
		return body
	}
	raw["model"] = alias
	out, err := json.Marshal(raw)
	if err != nil {
		return body
	}
	return out
}
