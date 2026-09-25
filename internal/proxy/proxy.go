package proxy

import (
	"bytes"
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/javded-itres/mikrollm/internal/auth"
	"github.com/javded-itres/mikrollm/internal/domain"
	"github.com/javded-itres/mikrollm/internal/guard"
	"github.com/javded-itres/mikrollm/internal/hubclient"
	"github.com/javded-itres/mikrollm/internal/params"
	"github.com/javded-itres/mikrollm/internal/ports"
	"github.com/javded-itres/mikrollm/internal/promptcache"
	"github.com/javded-itres/mikrollm/internal/queue"
)

type HubDial interface {
	URL() string
}

type Proxy struct {
	st       ports.Store
	health   ports.Health
	keys     ports.Auth
	client   ports.HTTPDoer
	queues   *queue.Engine
	hubRelay string
	hub      HubDial
	rr       atomic.Uint64
}

func New(st ports.Store, h ports.Health, keys ports.Auth, client ports.HTTPDoer) *Proxy {
	if client == nil {
		client = &http.Client{
			Timeout:       0,
			CheckRedirect: domain.NoRedirect,
			Transport: &http.Transport{
				ResponseHeaderTimeout: 10 * time.Minute,
				IdleConnTimeout:       90 * time.Second,
				MaxIdleConns:          32,
			},
		}
	}
	return &Proxy{st: st, health: h, keys: keys, client: client}
}

func (p *Proxy) SetQueue(e *queue.Engine) { p.queues = e }

func (p *Proxy) SetHubRelay(secret string) { p.hubRelay = secret }

func (p *Proxy) SetHubDial(h HubDial) { p.hub = h }

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
	if p.hubRelay != "" {
		got := r.Header.Get(hubclient.RelayHeader)
		if subtle.ConstantTimeCompare([]byte(got), []byte(p.hubRelay)) == 1 {
			return domain.APIKey{Name: "hub", Prefix: "hub", AllowedModels: []string{"*"}, Enabled: true}, true
		}
	}
	k, err := p.keys.Authenticate(auth.Bearer(r))
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]any{"message": "invalid api key", "type": "auth"}})
		return domain.APIKey{}, false
	}
	if !rpmExempt(r) && !p.keys.AllowRPM(k) {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": map[string]any{"message": "rate limit", "type": "rate_limit"}})
		return domain.APIKey{}, false
	}
	return k, true
}

func rpmExempt(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	p := r.URL.Path
	return strings.HasPrefix(p, "/v1/videos/") || strings.HasPrefix(p, "/admin/videos/")
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
		ID                  string   `json:"id"`
		Object              string   `json:"object"`
		OwnedBy             string   `json:"owned_by"`
		ContextLength       int      `json:"context_length,omitempty"`
		MaxModelLen         int      `json:"max_model_len,omitempty"`
		MaxTokens           int      `json:"max_tokens,omitempty"`
		MaxInputTokens      int      `json:"max_input_tokens,omitempty"`
		Provider            string   `json:"provider,omitempty"`
		InputCostPerToken   float64  `json:"input_cost_per_token,omitempty"`
		OutputCostPerToken  float64  `json:"output_cost_per_token,omitempty"`
		SupportedGeneration []string `json:"supported_generation,omitempty"`
		Mode                string   `json:"mode,omitempty"`
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
		media := domain.MergeMedia(m.Media, meta.Media)
		if len(media) == 0 {
			media = domain.InferMedia(m.Alias, nil)
			media = domain.MergeMedia(media, domain.InferMedia(m.UpstreamName, nil))
		}
		it.SupportedGeneration = media
		if domain.HasMedia(media, domain.MediaVideo) {
			it.Mode = "video_generation"
		} else if domain.HasMedia(media, domain.MediaImage) {
			it.Mode = "image_generation"
		}
		out.Data = append(out.Data, it)
	}
	qs, _ := p.st.ListQueues()
	for _, q := range qs {
		if !q.Enabled {
			continue
		}
		for _, a := range q.AllAliases() {
			if a == "" || !auth.ModelAllowed(k, a) {
				continue
			}
			out.Data = append(out.Data, item{ID: a, Object: "model", OwnedBy: "queue", Provider: "Очередь"})
		}
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

func isChatPath(path string) bool {
	return path == "/v1/chat/completions" || path == "/api/chat"
}

func (p *Proxy) exclusiveMedia(alias string) string {
	var media []string
	upstream := alias
	if m, err := p.st.GetModelByAlias(alias); err == nil {
		media = domain.MergeMedia(media, m.Media)
		if m.UpstreamName != "" {
			upstream = m.UpstreamName
		}
	}
	meta := p.catalogMeta(upstream, alias)
	media = domain.MergeMedia(media, meta.Media)
	if len(media) == 0 {
		media = domain.MergeMedia(domain.InferMedia(alias, nil), domain.InferMedia(upstream, nil))
	}
	return domain.ExclusiveMedia(media)
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
	qs, _ := p.st.ListQueues()
	for _, q := range qs {
		if !q.Enabled {
			continue
		}
		for _, a := range q.AllAliases() {
			if a != "" {
				out[a] = struct{}{}
			}
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
	if isChatPath(path) {
		if kind := p.exclusiveMedia(mb.Model); kind == domain.MediaVideo {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{
				"message": "Это видео-модель. В админке выберите тип «Видео» — POST /v1/videos, не чат.",
				"type":    "invalid_request_error",
				"code":    "invalid_value",
				"param":   "model",
			}})
			return
		}
	}
	if k.ID != 0 {
		p.st.TouchKey(k.ID)
	}

	ctx := domain.WithSessionID(r.Context(), r.Header.Get("X-Session-Id"))

	if p.queues != nil {
		if q, ok := p.queues.Lookup(mb.Model); ok {
			if needKey && !queueAllowed(k, q, mb.Model) {
				writeJSON(w, http.StatusForbidden, map[string]any{"error": map[string]any{"message": "model not allowed for this key"}})
				return
			}
			p.queues.Handle(ctx, w, k, path, body, q.Alias)
			return
		}
	}

	if needKey && !auth.ModelAllowed(k, mb.Model) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": map[string]any{"message": "model not allowed for this key"}})
		return
	}

	_, _, status, err := p.Forward(ctx, w, k, path, body, mb.Model)
	if err != nil && status == 0 {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": map[string]any{"message": err.Error()}})
	}
}

func (p *Proxy) TryRoute(model string) (backend, provider, upstream string, err error) {
	b, up, err := p.pick(model)
	if err != nil {
		return "", "", "", err
	}
	meta := p.catalogMeta(up, model)
	provider = meta.Provider
	if provider == "" {
		provider = b.Label()
	}
	return b.Name, provider, up, nil
}

func (p *Proxy) Forward(ctx context.Context, w http.ResponseWriter, k domain.APIKey, path string, body []byte, model string) (backend, provider string, status int, err error) {
	requested := model
	tried := map[string]bool{}
	var lastStatus int
	var lastMsg string
	filtered := body
	guarded := false
	var policies []domain.Policy
	for hop := 0; hop < 4; hop++ {
		if model == "" || tried[model] {
			break
		}
		tried[model] = true
		if hop > 0 && !auth.ModelAllowed(k, model) {
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
			return "", "", lastStatus, err
		}

		if !guarded {
			aliasName := ""
			if m, e := p.st.GetModelByAlias(model); e == nil && m.Enabled {
				aliasName = m.Alias
			}
			policies, _ = p.st.PoliciesFor(aliasName, domain.QueueAliasFrom(ctx), upstream)
			pre := guard.Apply(body, policies, domain.GuardPre)
			if pre.Block != nil {
				p.st.Log(k.Prefix, requested, "guard", 400, 0, 0, domain.TokenUsage{})
				w.Header().Set("X-MikroLLM-Guardrail", pre.Block.Policy)
				writeJSON(w, http.StatusBadRequest, guard.WriteError(pre.Block.Policy, pre.Block.Kind, pre.Block.Message))
				return "guard", "guard", 400, errors.New(pre.Block.Message)
			}
			filtered = pre.Body
			guarded = true
		}

		start := time.Now()
		p.health.Inc(b.ID)
		if b.KindNorm() == domain.KindHub {
			status, err := p.forwardHub(ctx, w, k, b, upstream, requested, filtered, start, path)
			p.health.Dec(b.ID)
			return b.Name, p.providerOf(b, model, upstream), status, err
		}
		upPath := rewriteUpstreamPath(b, path)
		mode := promptcache.ModeOff
		if b.KindNorm() == domain.KindOpenRouter {
			aliasMode := ""
			if m, e := p.st.GetModelByAlias(model); e == nil {
				aliasMode = m.PromptCache
			}
			mode = promptcache.Resolve(p.st.PromptCacheMode(), aliasMode)
		}
		meta := p.catalogMeta(upstream, model, requested)
		body := filtered
		switch kind := b.KindNorm(); kind {
		case domain.KindHub, domain.KindOpenComfy:
			// Hub peers apply their own alias profile; media is not chat-shaped.
		default:
			if m, e := p.st.GetModelByAlias(model); e == nil && strings.TrimSpace(m.Params) != "" {
				if prof, pe := params.ParseProfile(m.Params); pe == nil {
					body = params.Inject(body, prof, kind, b.NativeOllama() && path == "/api/chat")
				}
			}
		}
		payload, _ := promptcache.Prepare(promptcache.PrepInput{
			Body:      body,
			SendAs:    upstream,
			OrigModel: model,
			Kind:      b.KindNorm(),
			Provider:  meta.Provider,
			Mode:      mode,
		})
		method := http.MethodPost
		if strings.HasPrefix(path, "/v1/videos/") {
			method = http.MethodGet
			payload = nil
		}
		var rdr io.Reader
		if payload != nil {
			rdr = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(b.BaseURL, "/")+upPath, rdr)
		if err != nil {
			p.health.Dec(b.ID)
			http.Error(w, err.Error(), 500)
			return b.Name, p.providerOf(b, model, upstream), 500, err
		}
		if method != http.MethodGet {
			req.Header.Set("Content-Type", "application/json")
			req.ContentLength = int64(len(payload))
		}
		domain.ApplyUpstreamHeaders(req.Header, b)
		if b.KindNorm() == domain.KindOpenRouter {
			if sid := domain.SessionIDFrom(ctx); sid != "" {
				req.Header.Set("X-Session-Id", sid)
			}
		}

		resp, err := p.client.Do(req)
		if err != nil {
			p.health.Dec(b.ID)
			p.st.Log(k.Prefix, model, b.Name, 502, time.Since(start), 0, domain.TokenUsage{})
			lastStatus, lastMsg = http.StatusBadGateway, err.Error()
			if next := p.fallbackOf(model); next != "" && !tried[next] {
				model = next
				continue
			}
			writeJSON(w, lastStatus, map[string]any{"error": map[string]any{"message": lastMsg}})
			return b.Name, p.providerOf(b, model, upstream), lastStatus, err
		}

		if resp.StatusCode >= 400 {
			errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
			resp.Body.Close()
			p.health.Dec(b.ID)
			p.st.Log(k.Prefix, model, b.Name, resp.StatusCode, time.Since(start), int64(len(errBody)), domain.TokenUsage{})
			lastStatus, lastMsg = resp.StatusCode, strings.TrimSpace(string(errBody))
			if isBillingError(resp.StatusCode, errBody) {
				if next := p.fallbackOf(model); next != "" && !tried[next] {
					model = next
					continue
				}
			}
			copySafeHeaders(w.Header(), resp.Header)
			w.WriteHeader(resp.StatusCode)
			_, _ = w.Write(errBody)
			return b.Name, p.providerOf(b, model, upstream), resp.StatusCode, nil
		}

		if hop > 0 {
			w.Header().Set("X-MikroLLM-Fallback", requested+" -> "+model)
		}
		usage, nout, block := writeUpstream(w, resp, filtered, policies)
		p.health.Dec(b.ID)
		if block != nil {
			p.st.Log(k.Prefix, model, "guard", 400, time.Since(start), 0, domain.TokenUsage{})
			w.Header().Set("X-MikroLLM-Guardrail", block.Policy)
			writeJSON(w, http.StatusBadRequest, guard.WriteError(block.Policy, block.Kind, block.Message))
			return "guard", "guard", 400, errors.New(block.Message)
		}
		p.st.Log(k.Prefix, model, b.Name, resp.StatusCode, time.Since(start), nout, p.tokenUsage(usage, upstream, requested, p.providerOf(b, model, upstream)))
		return b.Name, p.providerOf(b, model, upstream), resp.StatusCode, nil
	}
	if lastStatus == 0 {
		lastStatus = http.StatusBadGateway
	}
	if lastMsg == "" {
		lastMsg = "no healthy backend for model " + requested
	}
	writeJSON(w, lastStatus, map[string]any{"error": map[string]any{"message": lastMsg}})
	return "", "", lastStatus, errors.New(lastMsg)
}

func (p *Proxy) providerOf(b domain.Backend, model, upstream string) string {
	meta := p.catalogMeta(upstream, model)
	if meta.Provider != "" {
		return meta.Provider
	}
	return b.Label()
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
	case "/v1/images/generations":
		if b.KindNorm() == domain.KindOpenRouter {
			return "/images"
		}
		return "/v1/images/generations"
	default:
		if strings.HasPrefix(path, "/v1/videos") {
			if b.KindNorm() == domain.KindOpenRouter {
				return strings.TrimPrefix(path, "/v1")
			}
			return path
		}
		return path
	}
}

func rewriteModel(body []byte, sendAs string) []byte {
	if sendAs == "" {
		return body
	}
	var raw map[string]any
	if json.Unmarshal(body, &raw) != nil {
		return body
	}
	if cur, _ := raw["model"].(string); cur == sendAs {
		return body
	}
	raw["model"] = sendAs
	out, err := json.Marshal(raw)
	if err != nil {
		return body
	}
	return out
}

func queueAllowed(k domain.APIKey, q domain.Queue, requested string) bool {
	if auth.ModelAllowed(k, requested) || auth.ModelAllowed(k, q.Alias) || auth.ModelAllowed(k, q.Name) {
		return true
	}
	for _, a := range q.AllAliases() {
		if auth.ModelAllowed(k, a) {
			return true
		}
	}
	return false
}

func hubRelaySuffix(path string) string {
	switch {
	case path == "/v1/images/generations":
		return "/images"
	case path == "/v1/videos":
		return "/videos"
	case strings.HasPrefix(path, "/v1/videos/"):
		rest := strings.TrimPrefix(path, "/v1/videos/")
		parts := strings.Split(rest, "/")
		for i, p := range parts {
			parts[i] = neturl.PathEscape(p)
		}
		return "/videos/" + strings.Join(parts, "/")
	default:
		return "/chat"
	}
}

func (p *Proxy) forwardHub(ctx context.Context, w http.ResponseWriter, k domain.APIKey, b domain.Backend, upstream, requested string, body []byte, start time.Time, path string) (int, error) {
	if p.hub == nil || strings.TrimSpace(p.hub.URL()) == "" {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": map[string]any{"message": "hub is not configured"}})
		return http.StatusBadGateway, errors.New("hub is not configured")
	}
	var raw map[string]any
	if json.Unmarshal(body, &raw) != nil {
		raw = map[string]any{}
	}
	wantStream := false
	if v, ok := raw["stream"].(bool); ok {
		wantStream = v
	}
	raw["model"] = upstream
	suffix := hubRelaySuffix(path)
	if suffix == "/chat" {
		raw["stream"] = false
	}
	payload, _ := json.Marshal(raw)
	u := strings.TrimRight(p.hub.URL(), "/") + "/v1/relay/" + neturl.PathEscape(b.BaseURL) + suffix
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(payload))
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": map[string]any{"message": err.Error()}})
		return 500, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		p.st.Log(k.Prefix, requested, b.Name, 502, time.Since(start), 0, domain.TokenUsage{})
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": map[string]any{"message": err.Error()}})
		return http.StatusBadGateway, err
	}
	defer resp.Body.Close()
	if routed := strings.TrimSpace(resp.Header.Get("X-MikroLLM-Routed-Model")); routed != "" {
		w.Header().Set("X-MikroLLM-Routed-Model", routed)
		b.Name = routed
	}
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 12<<20))
	code := resp.StatusCode
	if code == 0 {
		code = 200
	}
	p.st.Log(k.Prefix, requested, b.Name, code, time.Since(start), int64(len(out)), domain.TokenUsage{})
	if suffix == "/chat" && wantStream && code < 400 {
		res := hubclient.ParseChat(out)
		text, reasoning := res.Content, res.Reasoning
		if text == "" && len(res.ToolCalls) == 0 {
			text = reasoning
			reasoning = ""
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		delta := map[string]any{"role": "assistant", "content": text}
		if reasoning != "" {
			delta["reasoning"] = reasoning
		}
		if len(res.ToolCalls) > 0 {
			delta["tool_calls"] = res.ToolCalls
		}
		base := map[string]any{"object": "chat.completion.chunk", "created": time.Now().Unix()}
		if res.ID != "" {
			base["id"] = res.ID
		} else {
			base["id"] = "chatcmpl-hub"
		}
		if res.Model != "" {
			base["model"] = res.Model
		}
		finish := res.FinishReason
		if finish == "" {
			finish = "stop"
		}
		emit := func(choice map[string]any, usage json.RawMessage) {
			chunk := map[string]any{"choices": []any{choice}}
			for k, v := range base {
				chunk[k] = v
			}
			if len(usage) > 0 {
				chunk["usage"] = usage
			}
			b, _ := json.Marshal(chunk)
			_, _ = w.Write([]byte("data: " + string(b) + "\n\n"))
		}
		emit(map[string]any{"index": 0, "delta": delta, "finish_reason": nil}, nil)
		emit(map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finish}, res.Usage)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		return 200, nil
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/json"
	}
	w.Header().Set("Content-Type", ct)
	w.WriteHeader(code)
	_, _ = w.Write(out)
	if code >= 400 {
		return code, errors.New(strings.TrimSpace(string(out)))
	}
	return code, nil
}

func hubBackend(nodeID, nodeName, upstream string) (domain.Backend, string) {
	name := nodeName
	if name == "" {
		name = nodeID
	}
	return domain.Backend{Name: name, Kind: domain.KindHub, BaseURL: nodeID}, upstream
}

func (p *Proxy) pick(alias string) (domain.Backend, string, error) {
	if strings.HasPrefix(alias, "hub|") {
		rest := strings.TrimPrefix(alias, "hub|")
		i := strings.IndexByte(rest, '|')
		if i > 0 {
			b, up := hubBackend(rest[:i], "", rest[i+1:])
			return b, up, nil
		}
	}
	m, err := p.st.GetModelByAlias(alias)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return domain.Backend{}, "", err
	}
	var candidates []domain.Backend
	upstream := alias
	policy := "least_conn"
	if err == nil && m.Enabled {
		upstream = m.UpstreamName
		if m.HubNodeID == domain.HubAutoRouter {
			b, up := hubBackend("auto", "auto", domain.HubAutoAlias)
			return b, up, nil
		}
		if m.HubNodeID != "" {
			b, up := hubBackend(m.HubNodeID, m.HubNodeName, m.UpstreamName)
			return b, up, nil
		}
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
		if b, up, ok := p.pickHubCatalog(alias); ok {
			return b, up, nil
		}
		return domain.Backend{}, "", errors.New("no healthy backend for model " + alias)
	}
	return p.choose(candidates, policy), upstream, nil
}

func (p *Proxy) pickHubCatalog(alias string) (domain.Backend, string, bool) {
	bs, _ := p.st.ListBackends()
	var hit []domain.CatalogEntry
	for _, e := range p.health.Catalog(bs) {
		if e.HubNodeID != "" && e.HubOnline && e.Name == alias {
			hit = append(hit, e)
		}
	}
	if len(hit) == 0 {
		return domain.Backend{}, "", false
	}
	e := hit[0]
	b, up := hubBackend(e.HubNodeID, e.HubNodeName, e.Name)
	return b, up, true
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

const maxUsageBuffer = 1 << 20

func (p *Proxy) tokenUsage(u promptcache.Usage, upstream, requested, provider string) domain.TokenUsage {
	meta := p.catalogMeta(upstream, requested)
	saved, ok := promptcache.EstimateSaved(u, meta.PromptUSD, provider)
	return domain.TokenUsage{
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		CachedTokens:     u.CachedTokens,
		CacheWriteTokens: u.CacheWriteTokens,
		Upstream:         upstream,
		PromptUSD:        meta.PromptUSD,
		CacheDiscount:    u.CacheDiscount,
		HasDiscount:      u.HasDiscount,
		Cost:             u.Cost,
		HasCost:          u.HasCost,
		SavedUSD:         saved,
		HasSaved:         ok,
	}
}

func setCacheHeaders(h http.Header, u promptcache.Usage) {
	if u.CachedTokens > 0 {
		h.Set("X-MikroLLM-Cache-Tokens", strconv.Itoa(u.CachedTokens))
	}
	if u.CacheWriteTokens > 0 {
		h.Set("X-MikroLLM-Cache-Write-Tokens", strconv.Itoa(u.CacheWriteTokens))
	}
}

func writeUpstream(w http.ResponseWriter, resp *http.Response, filtered []byte, policies []domain.Policy) (promptcache.Usage, int64, *guard.Block) {
	defer resp.Body.Close()
	code := resp.StatusCode
	tmp := make([]byte, 32*1024)
	switch {
	case guard.IsStream(filtered):
		copySafeHeaders(w.Header(), resp.Header)
		w.WriteHeader(code)
		flusher, _ := w.(http.Flusher)
		sc := &promptcache.Scanner{}
		var nout int64
		for {
			n, rerr := resp.Body.Read(tmp)
			if n > 0 {
				wn, _ := w.Write(tmp[:n])
				nout += int64(wn)
				if flusher != nil {
					flusher.Flush()
				}
				sc.Feed(tmp[:n])
			}
			if rerr != nil {
				break
			}
		}
		return promptcache.ParseJSON(sc.LastUsage()), nout, nil

	case guard.HasPost(policies):
		cap := &memWriter{h: http.Header{}}
		copySafeHeaders(cap.Header(), resp.Header)
		cap.WriteHeader(code)
		for {
			n, rerr := resp.Body.Read(tmp)
			if n > 0 {
				_, _ = cap.Write(tmp[:n])
			}
			if rerr != nil {
				break
			}
		}
		post := guard.Apply(cap.buf.Bytes(), policies, domain.GuardPost)
		if post.Block != nil {
			return promptcache.Usage{}, 0, post.Block
		}
		u := promptcache.ParseJSON(post.Body)
		copySafeHeaders(w.Header(), cap.h)
		setCacheHeaders(w.Header(), u)
		outCode := cap.code
		if outCode == 0 {
			outCode = 200
		}
		w.WriteHeader(outCode)
		wn, _ := w.Write(post.Body)
		return u, int64(wn), nil

	default:
		var buf bytes.Buffer
		overflow := false
		flusher, _ := w.(http.Flusher)
		var nout int64
		for {
			n, rerr := resp.Body.Read(tmp)
			if n > 0 {
				if !overflow && buf.Len()+n <= maxUsageBuffer {
					buf.Write(tmp[:n])
				} else {
					if !overflow {
						overflow = true
						copySafeHeaders(w.Header(), resp.Header)
						w.WriteHeader(code)
						wn, _ := w.Write(buf.Bytes())
						nout += int64(wn)
						buf.Reset()
					}
					wn, _ := w.Write(tmp[:n])
					nout += int64(wn)
					if flusher != nil {
						flusher.Flush()
					}
				}
			}
			if rerr != nil {
				break
			}
		}
		if overflow {
			return promptcache.Usage{}, nout, nil
		}
		u := promptcache.ParseJSON(buf.Bytes())
		copySafeHeaders(w.Header(), resp.Header)
		setCacheHeaders(w.Header(), u)
		w.WriteHeader(code)
		wn, _ := w.Write(buf.Bytes())
		return u, int64(wn), nil
	}
}

type memWriter struct {
	h    http.Header
	code int
	buf  bytes.Buffer
}

func (m *memWriter) Header() http.Header {
	if m.h == nil {
		m.h = http.Header{}
	}
	return m.h
}
func (m *memWriter) WriteHeader(code int) { m.code = code }
func (m *memWriter) Write(p []byte) (int, error) {
	if m.code == 0 {
		m.code = 200
	}
	return m.buf.Write(p)
}

func copySafeHeaders(dst, src http.Header) {
	for hk, hv := range src {
		if dropResponseHeader(hk) {
			continue
		}
		for _, v := range hv {
			dst.Add(hk, v)
		}
	}
}

func dropResponseHeader(name string) bool {
	switch http.CanonicalHeaderKey(name) {
	case "Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization",
		"Te", "Trailer", "Transfer-Encoding", "Upgrade",
		"Set-Cookie", "Set-Cookie2", "Cookie", "Authorization", "Www-Authenticate",
		"Location", "Refresh", "Clear-Site-Data",
		"Access-Control-Allow-Origin", "Access-Control-Allow-Credentials",
		"Access-Control-Allow-Headers", "Access-Control-Allow-Methods",
		"Access-Control-Expose-Headers", "Access-Control-Max-Age",
		"Content-Security-Policy", "Content-Security-Policy-Report-Only",
		"X-Frame-Options", "X-Content-Type-Options":
		return true
	default:
		return false
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
