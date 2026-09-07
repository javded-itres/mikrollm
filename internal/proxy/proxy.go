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
	models, _ := p.st.ListModels()
	type item struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		OwnedBy string `json:"owned_by"`
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
		out.Data = append(out.Data, item{ID: m.Alias, Object: "model", OwnedBy: "mikrollm"})
	}
	if len(out.Data) == 0 {
		for name := range p.unionTags() {
			if auth.ModelAllowed(k, name) {
				out.Data = append(out.Data, item{ID: name, Object: "model", OwnedBy: "ollama"})
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
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

	b, upstream, err := p.pick(mb.Model)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": map[string]any{"message": err.Error()}})
		return
	}

	start := time.Now()
	p.health.Inc(b.ID)
	defer p.health.Dec(b.ID)
	if k.ID != 0 {
		p.st.TouchKey(k.ID)
	}

	payload := rewriteModel(body, mb.Model, upstream)
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, b.BaseURL+path, bytes.NewReader(payload))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = int64(len(payload))

	resp, err := p.client.Do(req)
	if err != nil {
		p.st.Log(k.Prefix, mb.Model, b.Name, 502, time.Since(start), 0)
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": map[string]any{"message": err.Error()}})
		return
	}
	defer resp.Body.Close()

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
	p.st.Log(k.Prefix, mb.Model, b.Name, resp.StatusCode, time.Since(start), nout)
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
