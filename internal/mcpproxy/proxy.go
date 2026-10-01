package mcpproxy

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
)

const maxUpstreamBody = 8 << 20

// Store is the upstream list and the hub membership flag.
type Store interface {
	GetMCPByName(name string) (domain.MCPUpstream, error)
	HubSettings() (domain.HubSettings, error)
}

// Allow reports whether the caller may use an MCP route. It writes the 401 itself.
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
			Timeout: 0,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
			Transport: &http.Transport{
				ResponseHeaderTimeout: 10 * time.Minute,
				IdleConnTimeout:       90 * time.Second,
			},
		},
	}
}

func (p *Proxy) Mount(mux *http.ServeMux) {
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete, http.MethodOptions} {
		mux.HandleFunc(method+" /mcp/u/{name}", p.local)
		mux.HandleFunc(method+" /mcp/u/{node}/{name}", p.remote)
	}
}

// Forward is one Streamable HTTP call to an upstream MCP server.
type Forward struct {
	Method      string
	Session     string
	Accept      string
	Protocol    string
	ContentType string
	Body        []byte
}

// Reply is the upstream status, the headers agents need, and the body.
type Reply struct {
	Status      int
	ContentType string
	Session     string
	Protocol    string
	Body        []byte
}

var upstreamClient = &http.Client{
	Timeout: 0,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
	Transport: &http.Transport{
		ResponseHeaderTimeout: 10 * time.Minute,
		IdleConnTimeout:       90 * time.Second,
	},
}

// Do calls the stored MCP URL. The caller bearer is never forwarded; the stored token is.
func Do(ctx context.Context, up domain.MCPUpstream, in Forward) (Reply, error) {
	method := strings.ToUpper(strings.TrimSpace(in.Method))
	if method == "" {
		method = http.MethodPost
	}
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodDelete:
	default:
		return Reply{Status: http.StatusMethodNotAllowed}, nil
	}
	var rdr io.Reader
	if len(in.Body) > 0 && method != http.MethodGet {
		rdr = bytes.NewReader(in.Body)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimSpace(up.URL), rdr)
	if err != nil {
		return Reply{}, err
	}
	if tok := strings.TrimSpace(up.Token); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	setSafe(req.Header, "Content-Type", in.ContentType)
	setSafe(req.Header, "Accept", in.Accept)
	setSafe(req.Header, "Mcp-Session-Id", in.Session)
	setSafe(req.Header, "Mcp-Protocol-Version", in.Protocol)
	resp, err := upstreamClient.Do(req)
	if err != nil {
		return Reply{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return Reply{}, errRedirect
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxUpstreamBody+1))
	if err != nil {
		return Reply{}, err
	}
	if len(body) > maxUpstreamBody {
		return Reply{Status: http.StatusRequestEntityTooLarge}, nil
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/json"
	}
	return Reply{
		Status:      resp.StatusCode,
		ContentType: safeHeader(ct, 200),
		Session:     safeHeader(resp.Header.Get("Mcp-Session-Id"), 200),
		Protocol:    safeHeader(resp.Header.Get("Mcp-Protocol-Version"), 80),
		Body:        body,
	}, nil
}

func (p *Proxy) local(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		writeOptions(w)
		return
	}
	if p.allow != nil && !p.allow(w, r) {
		return
	}
	up, err := p.store.GetMCPByName(r.PathValue("name"))
	if err != nil || !up.Enabled {
		writeErr(w, http.StatusNotFound, "unknown mcp")
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad body")
		return
	}
	rep, err := Do(r.Context(), up, Forward{
		Method: r.Method, Session: r.Header.Get("Mcp-Session-Id"),
		Accept: r.Header.Get("Accept"), Protocol: r.Header.Get("Mcp-Protocol-Version"),
		ContentType: r.Header.Get("Content-Type"), Body: body,
	})
	if err != nil {
		writeErr(w, http.StatusBadGateway, "upstream mcp")
		return
	}
	writeReply(w, rep)
}

func (p *Proxy) remote(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		writeOptions(w)
		return
	}
	if p.allow != nil && !p.allow(w, r) {
		return
	}
	cfg, err := p.store.HubSettings()
	if err != nil || !cfg.Enabled {
		writeErr(w, http.StatusNotFound, "unknown mcp")
		return
	}
	node := r.PathValue("node")
	name := r.PathValue("name")
	if !nodeIDOK(node) {
		writeErr(w, http.StatusNotFound, "unknown mcp")
		return
	}
	if _, err := domain.NormalizeMCPName(name); err != nil {
		writeErr(w, http.StatusNotFound, "unknown mcp")
		return
	}
	if p.hubURL == nil || strings.TrimSpace(p.hubURL()) == "" {
		writeErr(w, http.StatusBadGateway, "hub is not configured")
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad body")
		return
	}
	env := mcpEnvelope{
		Method: r.Method, Session: safeHeader(r.Header.Get("Mcp-Session-Id"), 200),
		Accept:      safeHeader(r.Header.Get("Accept"), 200),
		Protocol:    safeHeader(r.Header.Get("Mcp-Protocol-Version"), 80),
		ContentType: safeHeader(r.Header.Get("Content-Type"), 200),
	}
	if len(body) > 0 {
		env.Body = json.RawMessage(body)
		if !json.Valid(env.Body) {
			env.Body = nil
			wrapped, _ := json.Marshal(string(body))
			env.Body = json.RawMessage(wrapped)
		}
	}
	payload, _ := json.Marshal(env)
	u := strings.TrimRight(p.hubURL(), "/") + "/v1/relay/" + url.PathEscape(node) + "/mcp/" + url.PathEscape(name)
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
	out, _ := io.ReadAll(io.LimitReader(resp.Body, maxUpstreamBody+1))
	if len(out) > maxUpstreamBody {
		writeErr(w, http.StatusRequestEntityTooLarge, "mcp response too large")
		return
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/json"
	}
	writeReply(w, Reply{
		Status: resp.StatusCode, ContentType: safeHeader(ct, 200),
		Session:  safeHeader(resp.Header.Get("Mcp-Session-Id"), 200),
		Protocol: safeHeader(resp.Header.Get("Mcp-Protocol-Version"), 80),
		Body:     out,
	})
}

func writeReply(w http.ResponseWriter, rep Reply) {
	if rep.Status == 0 {
		rep.Status = http.StatusOK
	}
	if rep.ContentType != "" {
		w.Header().Set("Content-Type", rep.ContentType)
	}
	if rep.Session != "" {
		w.Header().Set("Mcp-Session-Id", rep.Session)
	}
	if rep.Protocol != "" {
		w.Header().Set("Mcp-Protocol-Version", rep.Protocol)
	}
	w.WriteHeader(rep.Status)
	_, _ = w.Write(rep.Body)
}

func writeOptions(w http.ResponseWriter) {
	w.Header().Set("Allow", "GET, POST, DELETE, OPTIONS")
	w.WriteHeader(http.StatusNoContent)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": msg})
}

func setSafe(h http.Header, key, val string) {
	val = safeHeader(val, 200)
	if val == "" {
		return
	}
	h.Set(key, val)
}

func safeHeader(v string, max int) string {
	v = strings.TrimSpace(v)
	if v == "" || strings.ContainsAny(v, "\r\n") {
		return ""
	}
	if max > 0 && len(v) > max {
		return ""
	}
	return v
}

func nodeIDOK(id string) bool {
	if len(id) < 8 || len(id) > 64 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}

// mcpEnvelope matches hubclient.MCPRelay. Kept here so the proxy does not import the hub client.
type mcpEnvelope struct {
	Method      string          `json:"method"`
	Session     string          `json:"session,omitempty"`
	Accept      string          `json:"accept,omitempty"`
	Protocol    string          `json:"protocol,omitempty"`
	ContentType string          `json:"content_type,omitempty"`
	Body        json.RawMessage `json:"body,omitempty"`
}

type redirectError struct{}

func (redirectError) Error() string { return "upstream redirect" }

var errRedirect error = redirectError{}
