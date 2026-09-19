package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javded-itres/mikrollm/internal/auth"
	"github.com/javded-itres/mikrollm/internal/domain"
	"github.com/javded-itres/mikrollm/internal/store"
)

const testToken = "mcp-unit-test-token-bbbbbbbbbbbbbbbbbbbb"

func testSrv(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.EnsureAdmin("secret99", true); err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnsureMCPToken(testToken, true); err != nil {
		t.Fatal(err)
	}
	s := New(Deps{Store: st, Auth: auth.New(st), Version: "test"})
	return s, st
}

func rpc(t *testing.T, s *Server, token, method string, params any) (int, map[string]any) {
	t.Helper()
	payload := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if params != nil {
		payload["params"] = params
	}
	raw, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.RemoteAddr = "192.168.88.10:1"
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func call(t *testing.T, s *Server, name string, args map[string]any) map[string]any {
	t.Helper()
	if args == nil {
		args = map[string]any{}
	}
	code, out := rpc(t, s, testToken, "tools/call", map[string]any{"name": name, "arguments": args})
	if code != 200 {
		t.Fatalf("%s http %d %+v", name, code, out)
	}
	if errObj, ok := out["error"].(map[string]any); ok {
		t.Fatalf("%s rpc error %+v", name, errObj)
	}
	res, _ := out["result"].(map[string]any)
	if res == nil {
		t.Fatalf("%s no result %+v", name, out)
	}
	if res["isError"] == true {
		t.Fatalf("%s isError %+v", name, res)
	}
	sc, _ := res["structuredContent"].(map[string]any)
	if sc == nil {
		t.Fatalf("%s no structuredContent %+v", name, res)
	}
	return sc
}

func TestMCPUnauthorized(t *testing.T) {
	s, _ := testSrv(t)
	code, _ := rpc(t, s, "", "initialize", map[string]any{"protocolVersion": proto2025, "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "1"}})
	if code != http.StatusUnauthorized {
		t.Fatalf("code %d", code)
	}
	code, _ = rpc(t, s, "sk-wrong", "tools/list", nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("bad token %d", code)
	}
}

func TestMCPInitializeAndTools(t *testing.T) {
	s, _ := testSrv(t)
	code, out := rpc(t, s, testToken, "initialize", map[string]any{
		"protocolVersion": proto2025, "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "t", "version": "1"},
	})
	if code != 200 {
		t.Fatalf("init %d %+v", code, out)
	}
	res := out["result"].(map[string]any)
	if res["protocolVersion"] != proto2025 {
		t.Fatalf("proto %+v", res)
	}
	info := res["serverInfo"].(map[string]any)
	if info["name"] != "mikrollm" {
		t.Fatalf("info %+v", info)
	}
	code, out = rpc(t, s, testToken, "tools/list", map[string]any{})
	if code != 200 {
		t.Fatalf("list %d", code)
	}
	tools := out["result"].(map[string]any)["tools"].([]any)
	if len(tools) < 15 {
		t.Fatalf("tools %d", len(tools))
	}
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "192.168.88.10:1"
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("notify %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.RemoteAddr = "192.168.88.10:1"
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET %d", rec.Code)
	}
}

func TestMCPProvidersModelsQueuesKeysLogs(t *testing.T) {
	s, st := testSrv(t)
	sc := call(t, s, "upsert_provider", map[string]any{
		"name": "mac-82", "base_url": "http://192.168.88.82:11434", "kind": "ollama",
	})
	id := int64(sc["id"].(float64))
	if id <= 0 {
		t.Fatalf("id %+v", sc)
	}
	sc = call(t, s, "list_providers", nil)
	provs := sc["providers"].([]any)
	if len(provs) != 1 {
		t.Fatalf("providers %+v", sc)
	}
	if provs[0].(map[string]any)["has_token"] != false {
		t.Fatalf("token leaked? %+v", provs[0])
	}

	sc = call(t, s, "upsert_provider", map[string]any{"id": id, "enabled": false})
	if sc["enabled"] != false {
		t.Fatalf("disable %+v", sc)
	}
	b, err := st.GetBackend(id)
	if err != nil || b.Enabled {
		t.Fatalf("store enabled %+v %v", b, err)
	}
	sc = call(t, s, "upsert_provider", map[string]any{"id": id, "name": "mac-82"})
	if sc["enabled"] != false {
		t.Fatalf("omit enabled must keep off %+v", sc)
	}
	sc = call(t, s, "upsert_provider", map[string]any{"id": id, "enabled": true})
	if sc["enabled"] != true {
		t.Fatalf("enable %+v", sc)
	}

	sc = call(t, s, "save_policy", map[string]any{
		"name": "inj", "kind": "prompt_injection", "aliases": []any{"fast"},
	})
	if sc["kind"] != "prompt_injection" {
		t.Fatalf("policy %+v", sc)
	}
	sc = call(t, s, "list_policies", nil)
	if len(sc["policies"].([]any)) != 1 {
		t.Fatalf("policies %+v", sc)
	}

	sc = call(t, s, "save_model", map[string]any{
		"alias": "fast", "upstream_name": "qwen3", "backend_ids": []any{id}, "lb_policy": "least_conn",
	})
	if sc["alias"] != "fast" {
		t.Fatalf("model %+v", sc)
	}
	sc = call(t, s, "list_models", nil)
	if len(sc["models"].([]any)) != 1 {
		t.Fatalf("models %+v", sc)
	}

	sc = call(t, s, "save_queue", map[string]any{
		"name": "itres", "alias": "coder", "overflow_after": 2,
		"steps": []any{
			map[string]any{"model_alias": "fast", "max_concurrent": 2},
		},
		"extra_aliases": []any{"itres-coder"},
	})
	q := sc["queue"].(map[string]any)
	if q["alias"] != "coder" {
		t.Fatalf("queue %+v", sc)
	}
	extras, _ := q["extra_aliases"].([]any)
	if len(extras) != 1 || extras[0] != "itres-coder" {
		t.Fatalf("extras %+v", q)
	}
	sc = call(t, s, "list_queues", nil)
	if len(sc["queues"].([]any)) != 1 {
		t.Fatalf("queues %+v", sc)
	}

	sc = call(t, s, "create_key", map[string]any{"name": "agent", "all_models": true, "rpm": 60})
	secret, _ := sc["key"].(string)
	if !strings.HasPrefix(secret, "sk-") {
		t.Fatalf("key %+v", sc)
	}
	sc = call(t, s, "list_keys", nil)
	keys := sc["keys"].([]any)
	if len(keys) != 1 {
		t.Fatalf("keys %+v", sc)
	}
	if _, ok := keys[0].(map[string]any)["key"]; ok {
		t.Fatal("list_keys must not return secret")
	}
	if _, ok := keys[0].(map[string]any)["key_hash"]; ok {
		t.Fatal("list_keys must not return hash")
	}

	st.Log("sk-agentxx", "fast", "mac-82", 200, 12*1e6, 100, domain.TokenUsage{CachedTokens: 8, SavedUSD: 0.1, HasSaved: true})
	st.Log("sk-agentxx", "fast", "mac-82", 502, 40*1e6, 20, domain.TokenUsage{})
	sc = call(t, s, "list_logs", map[string]any{"status_class": "5xx"})
	if sc["count"].(float64) != 1 {
		t.Fatalf("logs %+v", sc)
	}
	sc = call(t, s, "log_stats", nil)
	if sc["n"].(float64) != 2 || sc["errors"].(float64) != 1 {
		t.Fatalf("stats %+v", sc)
	}

	sc = call(t, s, "get_status", nil)
	if sc["aliases"].(float64) != 1 {
		t.Fatalf("status %+v", sc)
	}

	code, out := rpc(t, s, testToken, "tools/call", map[string]any{
		"name": "delete_model", "arguments": map[string]any{"alias": "missing-nope"},
	})
	if code != 200 {
		t.Fatalf("err http %d", code)
	}
	res := out["result"].(map[string]any)
	if res["isError"] != true {
		t.Fatalf("want isError %+v", out)
	}

	sc = call(t, s, "delete_queue", map[string]any{"name": "itres"})
	if sc["ok"] != true {
		t.Fatalf("del queue %+v", sc)
	}
	sc = call(t, s, "delete_model", map[string]any{"alias": "fast"})
	if sc["ok"] != true {
		t.Fatalf("del model %+v", sc)
	}
	sc = call(t, s, "delete_provider", map[string]any{"id": id})
	if sc["ok"] != true {
		t.Fatalf("del provider %+v", sc)
	}
}

func TestMCPAdminPasswordFallback(t *testing.T) {
	s, _ := testSrv(t)
	code, out := rpc(t, s, "secret99", "ping", nil)
	if code != 200 {
		t.Fatalf("admin pw %d %+v", code, out)
	}
}

func TestMCPRotateInvalidatesOld(t *testing.T) {
	s, _ := testSrv(t)
	sc := call(t, s, "rotate_mcp_token", nil)
	neu, _ := sc["token"].(string)
	if !strings.HasPrefix(neu, "mcp-") {
		t.Fatalf("new %+v", sc)
	}
	code, _ := rpc(t, s, testToken, "ping", nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("old token still works %d", code)
	}
	code, _ = rpc(t, s, neu, "ping", nil)
	if code != 200 {
		t.Fatalf("new token %d", code)
	}
}

func TestSanitizeRejectsFileURL(t *testing.T) {
	s, _ := testSrv(t)
	code, out := rpc(t, s, testToken, "tools/call", map[string]any{
		"name": "upsert_provider", "arguments": map[string]any{
			"name": "bad", "base_url": "file:///etc/passwd",
		},
	})
	if code != 200 {
		t.Fatalf("http %d", code)
	}
	res := out["result"].(map[string]any)
	if res["isError"] != true {
		t.Fatalf("want reject %+v", out)
	}
}

func TestStoreMCPToken(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.EnsureAdmin("x", true); err != nil {
		t.Fatal(err)
	}
	gen, err := st.EnsureMCPToken("", false)
	if err != nil || !strings.HasPrefix(gen, "mcp-") {
		t.Fatalf("gen %q %v", gen, err)
	}
	again, err := st.EnsureMCPToken("", false)
	if err != nil || again != "" {
		t.Fatalf("second gen %q %v", again, err)
	}
	p, _ := st.MCPTokenPrefix()
	if !strings.HasPrefix(p, "mcp-") {
		t.Fatalf("prefix %q", p)
	}
}

func TestHostActionUnknownBackend(t *testing.T) {
	s, _ := testSrv(t)
	code, out := rpc(t, s, testToken, "tools/call", map[string]any{
		"name": "host_action", "arguments": map[string]any{
			"action": "load", "backend_id": 99, "model": "x",
		},
	})
	res := out["result"].(map[string]any)
	if code != 200 || res["isError"] != true {
		t.Fatalf("%d %+v", code, out)
	}
}

func TestSaveQueueKeepsStepsWhenOmitted(t *testing.T) {
	s, st := testSrv(t)
	id, _ := st.UpsertBackend("b", "http://127.0.0.1:11434", true, 1, "ollama", "")
	_, _ = st.SaveModel(domain.Model{Alias: "a", UpstreamName: "a", Enabled: true, BackendIDs: []int64{id}})
	sc := call(t, s, "save_queue", map[string]any{
		"name": "q", "alias": "chat",
		"steps": []any{map[string]any{"model_alias": "a", "max_concurrent": 3}},
	})
	qid := sc["queue"].(map[string]any)["id"].(float64)
	sc = call(t, s, "save_queue", map[string]any{"id": qid, "name": "q2", "alias": "chat"})
	steps := sc["queue"].(map[string]any)["steps"].([]any)
	if len(steps) != 1 {
		t.Fatalf("steps dropped %+v", sc)
	}
}
