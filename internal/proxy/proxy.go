package proxy

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/javded-itres/mikrollm/internal/auth"
	"github.com/javded-itres/mikrollm/internal/domain"
	"github.com/javded-itres/mikrollm/internal/ports"
)

type Proxy struct {
	st     ports.Store
	health ports.Health
	keys   ports.Auth
	client ports.HTTPDoer
	rr     atomic.Uint64
}

func New(st ports.Store, h ports.Health, keys ports.Auth, client ports.HTTPDoer) *Proxy {
	if client == nil {
		client = &http.Client{
			Timeout: 0,
			Transport: &http.Transport{
				ResponseHeaderTimeout: 10 * time.Minute,
				IdleConnTimeout:       90 * time.Second,
				MaxIdleConns:          32,
			},
		}
	}
	return &Proxy{st: st, health: h, keys: keys, client: client}
}

func (p *Proxy) Health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (p *Proxy) Ready(w http.ResponseWriter, _ *http.Request) {
	if p.health.HealthyCount() < 1 {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (p *Proxy) requireKey(w http.ResponseWriter, r *http.Request) (domain.APIKey, bool) {
	k, err := p.keys.Authenticate(auth.Bearer(r))
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]any{"message": "invalid api key", "type": "auth"}})
		return domain.APIKey{}, false
	}
	if !p.keys.AllowRPM(k) {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": map[string]any{"message": "rate limit", "type": "rate_limit"}})
		return domain.APIKey{}, false
	}
	return k, true
}

func (p *Proxy) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	p.forward(w, r, "/v1/chat/completions", true)
}

func (p *Proxy) OllamaChat(w http.ResponseWriter, r *http.Request) {
	p.forward(w, r, "/api/chat", true)
}

func (p *Proxy) ServeChat(w http.ResponseWriter, r *http.Request) {
	p.forward(w, r, "/v1/chat/completions", false)
}

func (p *Proxy) ListModels(w http.ResponseWriter, r *http.Request) {
	k, ok := p.requireKey(w, r)
	if !ok {
		return
	}
	detected := p.detectedContexts()
	models, _ := p.st.ListModels()
	type item struct {
		ID                 string  `json:"id"`
		Object             string  `json:"object"`
		OwnedBy            string  `json:"owned_by"`
		ContextLength      int     `json:"context_length,omitempty"`
		MaxModelLen        int     `json:"max_model_len,omitempty"`
		MaxTokens          int     `json:"max_tokens,omitempty"`
		MaxInputTokens     int     `json:"max_input_tokens,omitempty"`
		Provider           string  `json:"provider,omitempty"`
		InputCostPerToken  float64 `json:"input_cost_per_token,omitempty"`
		OutputCostPerToken float64 `json:"output_cost_per_token,omitempty"`
	}
	out := struct {
		Object string `json:"object"`
		Data   []item `json:"data"`
	}{Object: "list"}
	for _, m := range models {
		if !m.Enabled {
			continue
		}
		if !auth.ModelAllowed(k, m.Alias) {
			continue
		}
		ctx := m.ContextWindow(detected[m.UpstreamName])
		if ctx == 0 {
			ctx = detected[m.Alias]
		}
		meta := p.catalogMeta(m.UpstreamName, m.Alias)
		owned := meta.Provider
		if owned == "" {
			owned = "mikrollm"
		}
		it := item{
			ID: m.Alias, Object: "model", OwnedBy: owned, Provider: meta.Provider,
			ContextLength: ctx, MaxModelLen: ctx, MaxTokens: ctx, MaxInputTokens: ctx,
		}
		if meta.Priced {
			it.InputCostPerToken = meta.PromptUSD / 1_000_000
			it.OutputCostPerToken = meta.CompletionUSD / 1_000_000
		}
		out.Data = append(out.Data, it)
	}
	if len(out.Data) == 0 {
		for name := range p.unionTags() {
			if !auth.ModelAllowed(k, name) {
				continue
			}
			ctx := detected[name]
			out.Data = append(out.Data, item{
				ID: name, Object: "model", OwnedBy: "mikrollm",
				ContextLength: ctx, MaxModelLen: ctx, MaxTokens: ctx, MaxInputTokens: ctx,
			})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (p *Proxy) ModelInfo(w http.ResponseWriter, r *http.Request) {
	k, ok := p.requireKey(w, r)
	if !ok {
		return
	}
	detected := p.detectedContexts()
	models, _ := p.st.ListModels()
	type row struct {
		ModelName     string         `json:"model_name"`
		LiteLLMParams map[string]any `json:"litellm_params"`
		ModelInfo     map[string]any `json:"model_info"`
	}
	var data []row
	for _, m := range models {
		if !m.Enabled || !auth.ModelAllowed(k, m.Alias) {
			continue
		}
		ctx := m.ContextWindow(detected[m.UpstreamName])
		if ctx == 0 {
			ctx = detected[m.Alias]
		}
		meta := p.catalogMeta(m.UpstreamName, m.Alias)
		info := map[string]any{
			"id": m.Alias, "db_model": true, "key": m.UpstreamName,
		}
		if meta.Provider != "" {
			info["provider"] = meta.Provider
		}
		if m.Fallback != "" {
			info["fallback"] = m.Fallback
		}
		if ctx > 0 {
			info["max_tokens"] = ctx
			info["max_input_tokens"] = ctx
			info["max_output_tokens"] = ctx
		}
		if meta.Priced {
			info["input_cost_per_token"] = meta.PromptUSD / 1_000_000
			info["output_cost_per_token"] = meta.CompletionUSD / 1_000_000
		}
		prov := meta.Provider
		if prov == "" {
			prov = "mikrollm"
		}
		data = append(data, row{
			ModelName: m.Alias,
			LiteLLMParams: map[string]any{
				"model": m.UpstreamName, "custom_llm_provider": prov,
			},
			ModelInfo: info,
		})
	}
	if len(data) == 0 {
		for name := range p.unionTags() {
			if !auth.ModelAllowed(k, name) {
				continue
			}
			ctx := detected[name]
			info := map[string]any{"id": name, "db_model": false, "key": name}
			if ctx > 0 {
				info["max_tokens"] = ctx
				info["max_input_tokens"] = ctx
				info["max_output_tokens"] = ctx
			}
			data = append(data, row{
				ModelName:     name,
				LiteLLMParams: map[string]any{"model": name, "custom_llm_provider": "mikrollm"},
				ModelInfo:     info,
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

func (p *Proxy) detectedContexts() map[string]int {
	out := map[string]int{}
	for _, e := range p.catalog() {
		if e.Context > out[e.Name] {
			out[e.Name] = e.Context
		}
	}
	return out
}

func (p *Proxy) catalog() []domain.CatalogEntry {
	backends, _ := p.st.ListBackends()
	return p.health.Catalog(backends)
}

func (p *Proxy) catalogMeta(names ...string) domain.CatalogEntry {
	cat := p.catalog()
	for _, name := range names {
		if name == "" {
			continue
		}
		for _, e := range cat {
			if e.Name == name {
				return e
			}
		}
	}
	return domain.CatalogEntry{}
}

func (p *Proxy) OllamaTags(w http.ResponseWriter, r *http.Request) {
	k, ok := p.requireKey(w, r)
	if !ok {
		return
	}
	seen := map[string]struct{}{}
	var models []map[string]any
	for name := range p.unionTags() {
		if !auth.ModelAllowed(k, name) {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		models = append(models, map[string]any{"name": name, "model": name})
	}
	if models == nil {
		models = []map[string]any{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

func (p *Proxy) unionTags() map[string]struct{} {
	out := map[string]struct{}{}
	backends, _ := p.st.ListBackends()
	for _, b := range backends {
		st := p.health.Get(b.ID)
		if !st.Healthy {
			continue
		}
		for _, n := range st.Models {
			out[n] = struct{}{}
		}
	}
	models, _ := p.st.ListModels()
	for _, m := range models {
		if m.Enabled {
			out[m.Alias] = struct{}{}
		}
	}
	return out
}

type modelBody struct {
	Model string `json:"model"`
}

func (p *Proxy) forward(w http.ResponseWriter, r *http.Request, path string, needKey bool) {
	var k domain.APIKey
	if needKey {
		var ok bool
		k, ok = p.requireKey(w, r)
		if !ok {
			return
		}
	} else {
		k = domain.APIKey{Name: "admin", Prefix: "admin", AllowedModels: []string{"*"}, Enabled: true}
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	var mb modelBody
	_ = json.Unmarshal(body, &mb)
	if mb.Model == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "model is required"}})
		return
	}
	if needKey && !auth.ModelAllowed(k, mb.Model) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": map[string]any{"message": "model not allowed for this key"}})
		return
	}

	if k.ID != 0 {
		p.st.TouchKey(k.ID)
	}

	model := mb.Model
	requested := mb.Model
	tried := map[string]bool{}
	var lastStatus int
	var lastMsg string
	for hop := 0; hop < 4; hop++ {
		if model == "" || tried[model] {
			break
		}
		tried[model] = true
		if needKey && hop > 0 && !auth.ModelAllowed(k, model) {
			break
		}
		b, upstream, err := p.pick(model)
		if err != nil {
			lastStatus, lastMsg = http.StatusBadGateway, err.Error()
			if next := p.fallbackOf(model); next != "" && !tried[next] {
				model = next
				continue
			}
			writeJSON(w, lastStatus, map[string]any{"error": map[string]any{"message": lastMsg}})
			return
		}

		start := time.Now()
		p.health.Inc(b.ID)
		upPath := rewriteUpstreamPath(b, path)
		payload := rewriteModel(body, requested, upstream)
		req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, strings.TrimRight(b.BaseURL, "/")+upPath, bytes.NewReader(payload))
		if err != nil {
			p.health.Dec(b.ID)
			http.Error(w, err.Error(), 500)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		domain.ApplyUpstreamHeaders(req.Header, b)
		req.ContentLength = int64(len(payload))

		resp, err := p.client.Do(req)
		if err != nil {
			p.health.Dec(b.ID)
			p.st.Log(k.Prefix, model, b.Name, 502, time.Since(start), 0)
			lastStatus, lastMsg = http.StatusBadGateway, err.Error()
			if next := p.fallbackOf(model); next != "" && !tried[next] {
				model = next
				continue
			}
			writeJSON(w, lastStatus, map[string]any{"error": map[string]any{"message": lastMsg}})
			return
		}

		if resp.StatusCode >= 400 {
			errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
			resp.Body.Close()
			p.health.Dec(b.ID)
			p.st.Log(k.Prefix, model, b.Name, resp.StatusCode, time.Since(start), int64(len(errBody)))
			lastStatus, lastMsg = resp.StatusCode, strings.TrimSpace(string(errBody))
			if isBillingError(resp.StatusCode, errBody) {
				if next := p.fallbackOf(model); next != "" && !tried[next] {
					model = next
					continue
				}
			}
			for hk, hv := range resp.Header {
				if strings.EqualFold(hk, "Connection") || strings.EqualFold(hk, "Transfer-Encoding") {
					continue
				}
				for _, v := range hv {
					w.Header().Add(hk, v)
				}
			}
			w.WriteHeader(resp.StatusCode)
			_, _ = w.Write(errBody)
			return
		}

		if hop > 0 {
			w.Header().Set("X-MikroLLM-Fallback", requested+" -> "+model)
		}
		for hk, hv := range resp.Header {
			if strings.EqualFold(hk, "Connection") || strings.EqualFold(hk, "Transfer-Encoding") {
				continue
			}
			for _, v := range hv {
				w.Header().Add(hk, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		flusher, _ := w.(http.Flusher)
		buf := make([]byte, 32*1024)
		var nout int64
		for {
			n, rerr := resp.Body.Read(buf)
			if n > 0 {
				wn, _ := w.Write(buf[:n])
				nout += int64(wn)
				if flusher != nil {
					flusher.Flush()
				}
			}
			if rerr != nil {
				break
			}
		}
		resp.Body.Close()
		p.health.Dec(b.ID)
		p.st.Log(k.Prefix, model, b.Name, resp.StatusCode, time.Since(start), nout)
		return
	}
	if lastStatus == 0 {
		lastStatus = http.StatusBadGateway
	}
	if lastMsg == "" {
		lastMsg = "no healthy backend for model " + requested
	}
	writeJSON(w, lastStatus, map[string]any{"error": map[string]any{"message": lastMsg}})
}

func (p *Proxy) fallbackOf(alias string) string {
	m, err := p.st.GetModelByAlias(alias)
	if err != nil {
		return ""
	}
	fb := strings.TrimSpace(m.Fallback)
	if fb == "" || fb == alias {
		return ""
	}
	return fb
}

func isBillingError(status int, body []byte) bool {
	if status == http.StatusPaymentRequired {
		return true
	}
	low := strings.ToLower(string(body))
	keys := []string{
		"insufficient credits", "payment required", "credit limit",
		"out of credits", "no credits", "quota exceeded", "exceeded your current quota",
		"billing", "subscription", "spend limit", "budget exceeded", "limit_remaining",
	}
	hit := false
	for _, k := range keys {
		if strings.Contains(low, k) {
			hit = true
			break
		}
	}
	if !hit {
		return false
	}
	if status == http.StatusForbidden || status == http.StatusTooManyRequests || (status >= 400 && status < 500) {
		return true
	}
	return false
}

func rewriteUpstreamPath(b domain.Backend, path string) string {
	switch path {
	case "/api/chat":
		if b.NativeOllama() {
			return "/api/chat"
		}
		return b.OpenAIChatPath()
	case "/v1/chat/completions":
		return b.OpenAIChatPath()
	default:
		return path
	}
}

func rewriteModel(body []byte, alias, upstream string) []byte {
	if upstream == "" || upstream == alias {
		return body
	}
	var raw map[string]any
	if json.Unmarshal(body, &raw) != nil {
		return body
	}
	raw["model"] = upstream
	out, err := json.Marshal(raw)
	if err != nil {
		return body
	}
	return out
}

func (p *Proxy) pick(alias string) (domain.Backend, string, error) {
	m, err := p.st.GetModelByAlias(alias)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return domain.Backend{}, "", err
	}
	var candidates []domain.Backend
	upstream := alias
	policy := "least_conn"
	if err == nil && m.Enabled {
		upstream = m.UpstreamName
		if m.LBPolicy != "" {
			policy = m.LBPolicy
		}
		for _, id := range m.BackendIDs {
			b, e := p.st.GetBackend(id)
			if e != nil || !b.Enabled {
				continue
			}
			if !p.health.Get(id).Healthy {
				continue
			}
			candidates = append(candidates, b)
		}
	}
	if len(candidates) == 0 {
		backends, _ := p.st.ListBackends()
		if len(backends) == 0 {
			return domain.Backend{}, "", errors.New("no backends configured")
		}
		for _, b := range backends {
			if !b.Enabled || !p.health.Get(b.ID).Healthy {
				continue
			}
			if p.health.HasModel(b.ID, alias) || len(p.health.Get(b.ID).Models) == 0 {
				candidates = append(candidates, b)
			}
		}
	}
	if len(candidates) == 0 {
		return domain.Backend{}, "", errors.New("no healthy backend for model " + alias)
	}
	return p.choose(candidates, policy), upstream, nil
}

func (p *Proxy) choose(list []domain.Backend, policy string) domain.Backend {
	switch policy {
	case "round_robin":
		i := p.rr.Add(1)
		return list[int(i-1)%len(list)]
	case "failover":
		return list[0]
	default:
		best := list[0]
		bestN := p.health.Conns(best.ID)
		for _, b := range list[1:] {
			n := p.health.Conns(b.ID)
			if n < bestN {
				best, bestN = b, n
			}
		}
		return best
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
