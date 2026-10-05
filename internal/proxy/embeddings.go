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

// Embeddings is OpenAI POST /v1/embeddings. Ollama models are sent to /api/embed.
func (p *Proxy) Embeddings(w http.ResponseWriter, r *http.Request) {
	p.serveEmbeddings(w, r, true)
}

// OllamaEmbed is POST /api/embed. The response stays in Ollama's shape.
func (p *Proxy) OllamaEmbed(w http.ResponseWriter, r *http.Request) {
	p.serveEmbeddings(w, r, false)
}

func (p *Proxy) serveEmbeddings(w http.ResponseWriter, r *http.Request, openAI bool) {
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
		Model string `json:"model"`
		Input any    `json:"input"`
	}
	if json.Unmarshal(body, &req) != nil || strings.TrimSpace(req.Model) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "model is required"}})
		return
	}
	if req.Input == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "input is required"}})
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
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "эмбеддинги через hub пока не проксируются"}})
		return
	}
	native := b.NativeOllama()
	upPath := "/v1/embeddings"
	payload := body
	if native {
		upPath = "/api/embed"
		raw, mErr := json.Marshal(map[string]any{"model": upstream, "input": req.Input})
		if mErr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": mErr.Error()}})
			return
		}
		payload = raw
	} else {
		payload = rewriteModel(body, upstream)
	}
	start := time.Now()
	p.health.Inc(b.ID)
	defer p.health.Dec(b.ID)
	httpReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, strings.TrimRight(b.BaseURL, "/")+upPath, bytes.NewReader(payload))
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
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if resp.StatusCode >= 400 {
		p.st.Log(k.Prefix, req.Model, b.Name, resp.StatusCode, time.Since(start), int64(len(respBody)), domain.TokenUsage{})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(respBody)
		return
	}
	out := respBody
	if openAI && native {
		converted, cErr := ollamaEmbedToOpenAI(req.Model, respBody)
		if cErr != nil {
			p.st.Log(k.Prefix, req.Model, b.Name, 502, time.Since(start), int64(len(respBody)), domain.TokenUsage{})
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": map[string]any{"message": cErr.Error()}})
			return
		}
		out = converted
	} else if !openAI && !native {
		converted, cErr := openAIEmbedToOllama(req.Model, respBody)
		if cErr != nil {
			p.st.Log(k.Prefix, req.Model, b.Name, 502, time.Since(start), int64(len(respBody)), domain.TokenUsage{})
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": map[string]any{"message": cErr.Error()}})
			return
		}
		out = converted
	}
	p.st.Log(k.Prefix, req.Model, b.Name, resp.StatusCode, time.Since(start), int64(len(out)), domain.TokenUsage{})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

func ollamaEmbedToOpenAI(model string, body []byte) ([]byte, error) {
	var raw struct {
		Embeddings      [][]float64 `json:"embeddings"`
		PromptEvalCount int         `json:"prompt_eval_count"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	data := make([]map[string]any, len(raw.Embeddings))
	for i, vec := range raw.Embeddings {
		data[i] = map[string]any{"object": "embedding", "index": i, "embedding": vec}
	}
	tokens := raw.PromptEvalCount
	return json.Marshal(map[string]any{
		"object": "list",
		"model":  model,
		"data":   data,
		"usage":  map[string]int{"prompt_tokens": tokens, "total_tokens": tokens},
	})
}

func openAIEmbedToOllama(model string, body []byte) ([]byte, error) {
	var raw struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
		Usage struct {
			PromptTokens int `json:"prompt_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	vecs := make([][]float64, len(raw.Data))
	for _, row := range raw.Data {
		if row.Index >= 0 && row.Index < len(vecs) {
			vecs[row.Index] = row.Embedding
		}
	}
	return json.Marshal(map[string]any{
		"model":             model,
		"embeddings":        vecs,
		"prompt_eval_count": raw.Usage.PromptTokens,
	})
}
