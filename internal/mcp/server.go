package mcp

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/javded-itres/mikrollm/internal/auth"
	"github.com/javded-itres/mikrollm/internal/domain"
	"github.com/javded-itres/mikrollm/internal/ports"
)

const (
	proto2025   = "2025-06-18"
	proto2025b  = "2025-03-26"
	proto2024   = "2024-11-05"
	protoLatest = proto2025
)

type snapshotter interface {
	Snapshot() []domain.QueueView
}

type Deps struct {
	Store   ports.Store
	Health  ports.Health
	Auth    ports.Auth
	Host    ports.Host
	Jobs    ports.Jobs
	Queues  snapshotter
	Version string
}

type Server struct {
	st      ports.Store
	health  ports.Health
	keys    ports.Auth
	host    ports.Host
	jobs    ports.Jobs
	queues  snapshotter
	version string
	tools   []toolDef
}

type toolDef struct {
	Name        string
	Description string
	Schema      map[string]any
	Fn          func(map[string]any) (any, error)
}

func New(d Deps) *Server {
	s := &Server{
		st: d.Store, health: d.Health, keys: d.Auth, host: d.Host, jobs: d.Jobs, queues: d.Queues,
		version: d.Version,
	}
	if s.version == "" {
		s.version = "dev"
	}
	s.tools = s.buildTools()
	return s
}

func (s *Server) Mount(mux *http.ServeMux) {
	h := s.ServeHTTP
	mux.HandleFunc("GET /mcp", h)
	mux.HandleFunc("POST /mcp", h)
	mux.HandleFunc("DELETE /mcp", h)
	mux.HandleFunc("OPTIONS /mcp", h)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.handlePOST(w, r)
	case http.MethodGet:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	case http.MethodDelete:
		if !s.authorize(w, r) {
			return
		}
		w.WriteHeader(http.StatusOK)
	case http.MethodOptions:
		w.Header().Set("Allow", "GET, POST, DELETE, OPTIONS")
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) authorize(w http.ResponseWriter, r *http.Request) bool {
	ip := auth.ClientIP(r)
	if s.keys != nil && s.keys.LoginBlocked(ip) {
		s.writeUnauthorized(w)
		return false
	}
	tok := auth.Bearer(r)
	ok := s.keys != nil && s.keys.ValidMCP(tok)
	if s.keys != nil {
		s.keys.RecordLogin(ip, ok)
	}
	if !ok {
		s.writeUnauthorized(w)
		return false
	}
	return true
}

func (s *Server) writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="MikroLLM MCP"`)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(rpcOut{
		JSONRPC: "2.0",
		Error:   &rpcErr{Code: -32001, Message: "unauthorized"},
	})
}

type rpcIn struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcOut struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcErr         `json:"error,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *Server) handlePOST(w http.ResponseWriter, r *http.Request) {
	if !s.authorize(w, r) {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		s.writeRPC(w, r, rpcOut{JSONRPC: "2.0", Error: &rpcErr{Code: -32700, Message: "parse error"}}, http.StatusBadRequest)
		return
	}
	var in rpcIn
	if err := json.Unmarshal(body, &in); err != nil || in.Method == "" {
		s.writeRPC(w, r, rpcOut{JSONRPC: "2.0", Error: &rpcErr{Code: -32700, Message: "parse error"}}, http.StatusBadRequest)
		return
	}
	notify := len(bytes.TrimSpace(in.ID)) == 0 || string(in.ID) == "null"
	out, code := s.dispatch(in)
	if notify {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if r.Header.Get("Mcp-Session-Id") == "" && in.Method == "initialize" {
		w.Header().Set("Mcp-Session-Id", newSessionID())
	} else if sid := r.Header.Get("Mcp-Session-Id"); sid != "" {
		w.Header().Set("Mcp-Session-Id", sid)
	}
	if v := negotiatedVersion(in); v != "" {
		w.Header().Set("MCP-Protocol-Version", v)
	}
	s.writeRPC(w, r, out, code)
}

func negotiatedVersion(in rpcIn) string {
	if in.Method != "initialize" {
		return ""
	}
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	_ = json.Unmarshal(in.Params, &p)
	return pickProtocol(p.ProtocolVersion)
}

func (s *Server) dispatch(in rpcIn) (rpcOut, int) {
	out := rpcOut{JSONRPC: "2.0", ID: in.ID}
	switch in.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(in.Params, &p)
		ver := pickProtocol(p.ProtocolVersion)
		out.Result = map[string]any{
			"protocolVersion": ver,
			"capabilities": map[string]any{
				"tools": map[string]any{"listChanged": false},
			},
			"serverInfo": map[string]any{
				"name":    "mikrollm",
				"title":   "MikroLLM",
				"version": s.version,
			},
			"instructions": mcpInstructions,
		}
		return out, http.StatusOK
	case "notifications/initialized", "notifications/cancelled":
		return out, http.StatusAccepted
	case "ping":
		out.Result = map[string]any{}
		return out, http.StatusOK
	case "tools/list":
		list := make([]map[string]any, 0, len(s.tools))
		for _, t := range s.tools {
			list = append(list, map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"inputSchema": t.Schema,
			})
		}
		out.Result = map[string]any{"tools": list}
		return out, http.StatusOK
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if len(in.Params) > 0 {
			if err := json.Unmarshal(in.Params, &p); err != nil {
				out.Error = &rpcErr{Code: -32602, Message: "invalid params"}
				return out, http.StatusOK
			}
		}
		if p.Arguments == nil {
			p.Arguments = map[string]any{}
		}
		fn := s.lookup(p.Name)
		if fn == nil {
			out.Error = &rpcErr{Code: -32601, Message: "unknown tool " + p.Name}
			return out, http.StatusOK
		}
		res, err := fn(p.Arguments)
		if err != nil {
			out.Result = toolResult(map[string]any{"error": err.Error()}, true)
			return out, http.StatusOK
		}
		out.Result = toolResult(res, false)
		return out, http.StatusOK
	case "resources/list":
		out.Result = map[string]any{"resources": []any{}}
		return out, http.StatusOK
	case "prompts/list":
		out.Result = map[string]any{"prompts": []any{}}
		return out, http.StatusOK
	default:
		out.Error = &rpcErr{Code: -32601, Message: "method not found"}
		return out, http.StatusOK
	}
}

func (s *Server) lookup(name string) func(map[string]any) (any, error) {
	for i := range s.tools {
		if s.tools[i].Name == name {
			return s.tools[i].Fn
		}
	}
	return nil
}

func toolResult(v any, isErr bool) map[string]any {
	raw, _ := json.Marshal(v)
	out := map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": string(raw)},
		},
		"structuredContent": v,
	}
	if isErr {
		out["isError"] = true
	}
	return out
}

func (s *Server) writeRPC(w http.ResponseWriter, r *http.Request, out rpcOut, code int) {
	raw, err := json.Marshal(out)
	if err != nil {
		http.Error(w, "encode", http.StatusInternalServerError)
		return
	}
	accept := r.Header.Get("Accept")
	wantSSE := strings.Contains(accept, "text/event-stream") && !strings.Contains(accept, "application/json")
	if wantSSE {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(code)
		_, _ = w.Write([]byte("event: message\ndata: "))
		_, _ = w.Write(raw)
		_, _ = w.Write([]byte("\n\n"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(raw)
	_, _ = w.Write([]byte("\n"))
}

func pickProtocol(client string) string {
	switch strings.TrimSpace(client) {
	case proto2024, proto2025b, proto2025, "2025-11-25":
		return client
	default:
		return protoLatest
	}
}

func newSessionID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

const mcpInstructions = `MikroLLM — шлюз к Ollama / vLLM / LM Studio / OpenRouter / Ollama Cloud. Сначала get_status. Провайдеры = бэкенды (upsert_provider). Модели = alias шлюза (save_model / connect_model). Очереди маршрутизируют клиентский alias по шагам (save_queue со steps). Ключи sk- ограничивают модели и RPM (create_key). Логи — list_logs и log_stats. pull/load асинхронны: смотрите list_jobs. Токены бэкендов и секреты ключей в ответах не повторяйте.`
