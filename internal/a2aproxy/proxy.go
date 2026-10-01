package a2aproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/javded-itres/mikrollm/internal/domain"
	"github.com/javded-itres/mikrollm/internal/mcpproxy"
)

// Store is the agent list and the hub membership flag.
type Store interface {
	GetA2AByName(name string) (domain.A2AUpstream, error)
	HubSettings() (domain.HubSettings, error)
}

// Allow reports whether the caller may use an agent route. It writes the 401 itself.
type Allow func(http.ResponseWriter, *http.Request) bool

type Proxy struct {
	store  Store
	allow  Allow
	hubURL func() string
	client *http.Client
}

func New(st Store, allow Allow, hubURL func() string) *Proxy {
	return &Proxy{
		store: st, allow: allow, hubURL: hubURL,
		client: &http.Client{
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
			Transport: &http.Transport{
				ResponseHeaderTimeout: 10 * time.Minute,
				IdleConnTimeout:       90 * time.Second,
			},
		},
	}
}

func (p *Proxy) Mount(mux *http.ServeMux) {
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete, http.MethodOptions} {
		mux.HandleFunc(method+" /a2a/u/{path...}", p.serve)
	}
}

func (p *Proxy) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.Header().Set("Allow", "GET, POST, DELETE, OPTIONS")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if p.allow != nil && !p.allow(w, r) {
		return
	}
	node, name, rest, remote := splitPath(r.PathValue("path"))
	if name == "" {
		writeErr(w, http.StatusNotFound, "unknown agent")
		return
	}
	if remote {
		p.remote(w, r, node, name, rest)
		return
	}
	up, err := p.store.GetA2AByName(name)
	if err != nil || !up.Enabled {
		writeErr(w, http.StatusNotFound, "unknown agent")
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad body")
		return
	}
	if IsCard(rest) {
		rep, err := FetchCard(r.Context(), up)
		if err != nil {
			writeErr(w, http.StatusBadGateway, "upstream agent")
			return
		}
		public := publicURL(r, "/a2a/u/"+url.PathEscape(name))
		rep.Body = RewriteCard(rep.Body, public, name)
		rep.ContentType = "application/json"
		writeReply(w, rep)
		return
	}
	target, ok := Join(up.URL, rest)
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown agent")
		return
	}
	rep, err := call(r.Context(), up.Token, target, forwardIn(r, body))
	if err != nil {
		writeErr(w, http.StatusBadGateway, "upstream agent")
		return
	}
	writeReply(w, rep)
}

func (p *Proxy) remote(w http.ResponseWriter, r *http.Request, node, name, rest string) {
	cfg, err := p.store.HubSettings()
	if err != nil || !cfg.Enabled || p.hubURL == nil || strings.TrimSpace(p.hubURL()) == "" {
		writeErr(w, http.StatusNotFound, "unknown agent")
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad body")
		return
	}
	env := map[string]any{
		"method": r.Method, "path": rest,
		"session":      r.Header.Get("Mcp-Session-Id"),
		"accept":       r.Header.Get("Accept"),
		"protocol":     r.Header.Get("Mcp-Protocol-Version"),
		"content_type": r.Header.Get("Content-Type"),
	}
	if len(body) > 0 {
		if json.Valid(body) {
			env["body"] = json.RawMessage(body)
		} else {
			env["body"] = string(body)
		}
	}
	payload, _ := json.Marshal(env)
	u := strings.TrimRight(p.hubURL(), "/") + "/v1/relay/" + url.PathEscape(node) + "/a2a/" + url.PathEscape(name)
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, u, bytes.NewReader(payload))
	if err != nil {
		writeErr(w, http.StatusBadGateway, "hub relay")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := p.client.Do(req)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "hub relay")
		return
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20+1))
	if len(out) > 8<<20 {
		writeErr(w, http.StatusRequestEntityTooLarge, "agent response too large")
		return
	}
	if IsCard(rest) && resp.StatusCode < 400 {
		out = RewriteCard(out, publicURL(r, "/a2a/u/"+url.PathEscape(node)+"/"+url.PathEscape(name)), name)
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" || IsCard(rest) {
		ct = "application/json"
	}
	writeReply(w, mcpproxy.Reply{Status: resp.StatusCode, ContentType: ct, Body: out})
}

func forwardIn(r *http.Request, body []byte) mcpproxy.Forward {
	return mcpproxy.Forward{
		Method: r.Method, Accept: r.Header.Get("Accept"),
		ContentType: r.Header.Get("Content-Type"), Body: body,
	}
}

func call(ctx context.Context, token, target string, in mcpproxy.Forward) (mcpproxy.Reply, error) {
	return mcpproxy.Do(ctx, domain.MCPUpstream{URL: target, Token: token}, in)
}

// FetchCard loads the upstream agent card. The RPC URL is not returned to the client.
func FetchCard(ctx context.Context, up domain.A2AUpstream) (mcpproxy.Reply, error) {
	var last mcpproxy.Reply
	var lastErr error
	for _, raw := range cardCandidates(up.URL) {
		rep, err := call(ctx, up.Token, raw, mcpproxy.Forward{Method: http.MethodGet, Accept: "application/json"})
		if err != nil {
			lastErr = err
			continue
		}
		last = rep
		if rep.Status > 0 && rep.Status < 300 && json.Valid(rep.Body) {
			return rep, nil
		}
	}
	if lastErr != nil && last.Status == 0 {
		return mcpproxy.Reply{}, lastErr
	}
	raw, _ := json.Marshal(map[string]any{
		"name": up.Name, "version": "1.0.0", "protocolVersion": "0.3.0",
		"capabilities": map[string]any{"streaming": true},
		"skills":       []any{},
	})
	return mcpproxy.Reply{Status: http.StatusOK, ContentType: "application/json", Body: raw}, nil
}

func cardCandidates(base string) []string {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil
	}
	origin := u.Scheme + "://" + u.Host
	list := []string{origin + "/.well-known/agent-card.json", origin + "/.well-known/agent.json"}
	if strings.Contains(u.Path, "agent-card.json") || strings.Contains(u.Path, "agent.json") {
		list = append([]string{u.String()}, list...)
	}
	return list
}

// IsCard reports an agent-card discovery path.
func IsCard(rest string) bool {
	rest = strings.Trim(rest, "/")
	return rest == ".well-known/agent-card.json" || rest == ".well-known/agent.json"
}

// Join appends a relative A2A path to the stored RPC URL.
func Join(base, rest string) (string, bool) {
	rest = strings.TrimSpace(rest)
	if strings.Contains(rest, "..") || strings.Contains(rest, "://") || strings.ContainsAny(rest, "\\?#") {
		return "", false
	}
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	rest = strings.Trim(rest, "/")
	if rest == "" {
		return base, base != ""
	}
	return base + "/" + rest, true
}

// RewriteCard points the card at this gateway and drops the upstream address.
func RewriteCard(body []byte, publicURL, name string) []byte {
	var m map[string]any
	if json.Unmarshal(body, &m) != nil || m == nil {
		m = map[string]any{"name": name, "version": "1.0.0", "protocolVersion": "0.3.0"}
	}
	m["url"] = publicURL
	rewriteList(m["additionalInterfaces"], publicURL)
	rewriteList(m["supportedInterfaces"], publicURL)
	m["securitySchemes"] = map[string]any{"gateway": map[string]any{"type": "http", "scheme": "bearer"}}
	m["security"] = []any{map[string]any{"gateway": []any{}}}
	out, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return out
}

func rewriteList(v any, publicURL string) {
	list, ok := v.([]any)
	if !ok {
		return
	}
	for _, item := range list {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if _, has := row["url"]; has {
			row["url"] = publicURL
		}
	}
}

// splitPath distinguishes a local agent from a hub node id plus agent name.
// Node ids are n and 32 hex characters.
func splitPath(raw string) (node, name, rest string, remote bool) {
	parts := strings.Split(strings.Trim(raw, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return "", "", "", false
	}
	if len(parts) >= 2 && nodeID(parts[0]) {
		if n, err := domain.NormalizeMCPName(parts[1]); err == nil {
			return parts[0], n, strings.Join(parts[2:], "/"), true
		}
	}
	n, err := domain.NormalizeMCPName(parts[0])
	if err != nil {
		return "", "", "", false
	}
	return "", n, strings.Join(parts[1:], "/"), false
}

func nodeID(id string) bool {
	if len(id) != 33 || id[0] != 'n' {
		return false
	}
	for _, r := range id[1:] {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f':
		default:
			return false
		}
	}
	return true
}

func publicURL(r *http.Request, path string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if p := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); p == "http" || p == "https" {
		scheme = p
	}
	return scheme + "://" + r.Host + path
}

func writeReply(w http.ResponseWriter, rep mcpproxy.Reply) {
	if rep.Status == 0 {
		rep.Status = http.StatusOK
	}
	if rep.ContentType != "" {
		w.Header().Set("Content-Type", rep.ContentType)
	}
	w.WriteHeader(rep.Status)
	_, _ = w.Write(rep.Body)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": msg})
}
