package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/javded-itres/mikrollm/internal/auth"
	"github.com/javded-itres/mikrollm/internal/health"
	"github.com/javded-itres/mikrollm/internal/store"
)

func setup(t *testing.T) (*store.Store, *health.Checker, *auth.Service, *Proxy, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.EnsureAdmin("secret", true); err != nil {
		t.Fatal(err)
	}
	h := health.New(st, nil)
	k := auth.New(st)
	return st, h, k, New(st, h, k, nil), dir
}

func TestUnauthorized(t *testing.T) {
	_, _, _, px, _ := setup(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"x"}`))
	rec := httptest.NewRecorder()
	px.ChatCompletions(rec, req)
	if rec.Code != 401 {
		t.Fatalf("code %d", rec.Code)
	}
}

func TestAllowlistAndProxy(t *testing.T) {
	st, h, _, px, _ := setup(t)
	var gotModel string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/version":
			w.Write([]byte(`{"version":"0"}`))
		case "/api/tags":
			_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{{"name": "qwen"}}})
		case "/v1/chat/completions":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			gotModel, _ = body["model"].(string)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id":"1"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(up.Close)
	bid, err := st.UpsertBackend("mac", up.URL, true, 1, "ollama", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.SaveModel(store.Model{Alias: "qwen", UpstreamName: "qwen", LBPolicy: "failover", Enabled: true, BackendIDs: []int64{bid}})
	if err != nil {
		t.Fatal(err)
	}
	plain, prefix, hash, err := auth.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.InsertKey(store.APIKey{Name: "t", Prefix: prefix, KeyHash: hash, AllowedModels: []string{"qwen"}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	h.CheckOnce()
	time.Sleep(10 * time.Millisecond)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"other","messages":[]}`))
	req.Header.Set("Authorization", "Bearer "+plain)
	rec := httptest.NewRecorder()
	px.ChatCompletions(rec, req)
	if rec.Code != 403 {
		t.Fatalf("want 403 got %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"qwen","messages":[]}`))
	req.Header.Set("Authorization", "Bearer "+plain)
	rec = httptest.NewRecorder()
	px.ChatCompletions(rec, req)
	if rec.Code != 200 {
		t.Fatalf("want 200 got %d %s", rec.Code, rec.Body.String())
	}
	if gotModel != "qwen" {
		t.Fatalf("upstream model %q", gotModel)
	}
}

func TestRewriteModelReplacesQueueAlias(t *testing.T) {
	in := []byte(`{"model":"itres-coder","messages":[{"role":"user","content":"hi"}]}`)
	out := rewriteModel(in, "ornith-1.5:35b")
	var raw map[string]any
	if json.Unmarshal(out, &raw) != nil {
		t.Fatal(string(out))
	}
	if raw["model"] != "ornith-1.5:35b" {
		t.Fatalf("model %v", raw["model"])
	}
	if same := rewriteModel(out, "ornith-1.5:35b"); string(same) != string(out) {
		t.Fatal("idempotent")
	}
}

func TestDropsUpstreamCookies(t *testing.T) {
	st, h, _, px, _ := setup(t)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/version":
			w.Write([]byte(`{"version":"0"}`))
		case "/api/tags":
			_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{{"name": "qwen"}}})
		case "/v1/chat/completions":
			w.Header().Set("Set-Cookie", "mikrollm_session=stolen; Path=/")
			w.Header().Set("Location", "https://evil.example/")
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id":"1"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(up.Close)
	bid, err := st.UpsertBackend("mac", up.URL, true, 1, "ollama", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.SaveModel(store.Model{Alias: "qwen", UpstreamName: "qwen", Enabled: true, BackendIDs: []int64{bid}}); err != nil {
		t.Fatal(err)
	}
	plain, prefix, hash, err := auth.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.InsertKey(store.APIKey{Name: "t", Prefix: prefix, KeyHash: hash, AllowedModels: []string{"*"}, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	h.CheckOnce()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"qwen"}`))
	req.Header.Set("Authorization", "Bearer "+plain)
	rec := httptest.NewRecorder()
	px.ChatCompletions(rec, req)
	if rec.Code != 200 {
		t.Fatalf("code %d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Set-Cookie") != "" {
		t.Fatalf("leaked cookie %q", rec.Header().Get("Set-Cookie"))
	}
	if rec.Header().Get("Location") != "" {
		t.Fatalf("leaked location %q", rec.Header().Get("Location"))
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("content-type %q", rec.Header().Get("Content-Type"))
	}
}

func TestServeChatSkipsAPIKey(t *testing.T) {
	st, h, _, px, _ := setup(t)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/version":
			w.Write([]byte(`{"version":"0"}`))
		case "/api/tags":
			_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{{"name": "qwen"}}})
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hi"}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(up.Close)
	bid, err := st.UpsertBackend("mac", up.URL, true, 1, "ollama", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.SaveModel(store.Model{Alias: "qwen", UpstreamName: "qwen", Enabled: true, BackendIDs: []int64{bid}}); err != nil {
		t.Fatal(err)
	}
	h.CheckOnce()
	req := httptest.NewRequest(http.MethodPost, "/admin/chat", strings.NewReader(`{"model":"qwen","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	px.ServeChat(rec, req)
	if rec.Code != 200 {
		t.Fatalf("code %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "hi") {
		t.Fatalf("body %s", rec.Body.String())
	}
}

func TestVLLMChatRewritesAPIChatAndSendsToken(t *testing.T) {
	st, h, _, px, _ := setup(t)
	var gotPath, gotAuth string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(200)
		case "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "Qwen/Qwen2.5-7B-Instruct"}}})
		case "/v1/chat/completions":
			gotPath = r.URL.Path
			gotAuth = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id":"1"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(up.Close)
	bid, err := st.UpsertBackend("gpu", up.URL, true, 1, "vllm", "vllm-secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.SaveModel(store.Model{
		Alias: "qwen", UpstreamName: "Qwen/Qwen2.5-7B-Instruct",
		Enabled: true, BackendIDs: []int64{bid},
	}); err != nil {
		t.Fatal(err)
	}
	plain, prefix, hash, err := auth.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.InsertKey(store.APIKey{Name: "t", Prefix: prefix, KeyHash: hash, AllowedModels: []string{"*"}, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	h.CheckOnce()
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(`{"model":"qwen","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer "+plain)
	rec := httptest.NewRecorder()
	px.OllamaChat(rec, req)
	if rec.Code != 200 {
		t.Fatalf("code %d %s", rec.Code, rec.Body.String())
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("upstream path %q", gotPath)
	}
	if gotAuth != "Bearer vllm-secret" {
		t.Fatalf("upstream auth %q", gotAuth)
	}
}

func TestOpenRouterChatPathAndHeaders(t *testing.T) {
	st, h, _, px, _ := setup(t)
	var gotPath, gotAuth, gotReferer string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "openai/gpt-4o-mini"}}})
		case "/chat/completions":
			gotPath = r.URL.Path
			gotAuth = r.Header.Get("Authorization")
			gotReferer = r.Header.Get("HTTP-Referer")
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id":"1"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(up.Close)
	bid, err := st.UpsertBackend("or", up.URL, true, 1, "openrouter", "sk-or-test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.SaveModel(store.Model{
		Alias: "fast", UpstreamName: "openai/gpt-4o-mini",
		Enabled: true, BackendIDs: []int64{bid},
	}); err != nil {
		t.Fatal(err)
	}
	plain, prefix, hash, err := auth.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.InsertKey(store.APIKey{Name: "t", Prefix: prefix, KeyHash: hash, AllowedModels: []string{"*"}, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	h.CheckOnce()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"fast","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer "+plain)
	rec := httptest.NewRecorder()
	px.ChatCompletions(rec, req)
	if rec.Code != 200 {
		t.Fatalf("code %d %s", rec.Code, rec.Body.String())
	}
	if gotPath != "/chat/completions" {
		t.Fatalf("upstream path %q", gotPath)
	}
	if gotAuth != "Bearer sk-or-test" {
		t.Fatalf("auth %q", gotAuth)
	}
	if gotReferer == "" {
		t.Fatal("missing HTTP-Referer")
	}
}

func TestOllamaCloudKeepsAPIChat(t *testing.T) {
	st, h, _, px, _ := setup(t)
	var gotPath string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{{"name": "gpt-oss:120b"}}})
		case "/api/chat":
			gotPath = r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"message":{"role":"assistant","content":"hi"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(up.Close)
	bid, err := st.UpsertBackend("oc", up.URL, true, 1, "ollama-cloud", "ol-key")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.SaveModel(store.Model{
		Alias: "gpt-oss:120b", UpstreamName: "gpt-oss:120b",
		Enabled: true, BackendIDs: []int64{bid},
	}); err != nil {
		t.Fatal(err)
	}
	plain, prefix, hash, err := auth.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.InsertKey(store.APIKey{Name: "t", Prefix: prefix, KeyHash: hash, AllowedModels: []string{"*"}, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	h.CheckOnce()
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(`{"model":"gpt-oss:120b","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer "+plain)
	rec := httptest.NewRecorder()
	px.OllamaChat(rec, req)
	if rec.Code != 200 {
		t.Fatalf("code %d %s", rec.Code, rec.Body.String())
	}
	if gotPath != "/api/chat" {
		t.Fatalf("upstream path %q", gotPath)
	}
}

func TestModelInfoContext(t *testing.T) {
	st, h, _, px, _ := setup(t)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/version":
			w.Write([]byte(`{"version":"0"}`))
		case "/api/tags":
			_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{{"name": "qwen"}}})
		case "/api/show":
			_ = json.NewEncoder(w).Encode(map[string]any{"model_info": map[string]any{"qwen.context_length": 32768}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(up.Close)
	bid, err := st.UpsertBackend("mac", up.URL, true, 1, "ollama", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.SaveModel(store.Model{
		Alias: "fast", UpstreamName: "qwen", Enabled: true, BackendIDs: []int64{bid}, MaxContext: 65536,
	}); err != nil {
		t.Fatal(err)
	}
	plain, prefix, hash, err := auth.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.InsertKey(store.APIKey{Name: "t", Prefix: prefix, KeyHash: hash, AllowedModels: []string{"*"}, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	h.CheckOnce()
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+plain)
	rec := httptest.NewRecorder()
	px.ListModels(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"max_input_tokens":65536`) {
		t.Fatalf("models %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/model/info", nil)
	req.Header.Set("Authorization", "Bearer "+plain)
	rec = httptest.NewRecorder()
	px.ModelInfo(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"max_input_tokens":65536`) || !strings.Contains(rec.Body.String(), `"model_name":"fast"`) {
		t.Fatalf("info %d %s", rec.Code, rec.Body.String())
	}
}

func TestBillingFallback(t *testing.T) {
	st, h, _, px, _ := setup(t)
	var got []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/version":
			w.Write([]byte(`{"version":"0"}`))
		case "/api/tags":
			_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{{"name": "paid"}, {"name": "cheap"}}})
		case "/v1/chat/completions":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			m, _ := body["model"].(string)
			got = append(got, m)
			if m == "paid" {
				w.WriteHeader(http.StatusPaymentRequired)
				w.Write([]byte(`{"error":{"message":"Insufficient credits","code":402}}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id":"ok"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(up.Close)
	bid, err := st.UpsertBackend("mac", up.URL, true, 1, "ollama", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.SaveModel(store.Model{Alias: "cheap", UpstreamName: "cheap", Enabled: true, BackendIDs: []int64{bid}}); err != nil {
		t.Fatal(err)
	}
	if _, err = st.SaveModel(store.Model{Alias: "paid", UpstreamName: "paid", Fallback: "cheap", Enabled: true, BackendIDs: []int64{bid}}); err != nil {
		t.Fatal(err)
	}
	plain, prefix, hash, err := auth.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.InsertKey(store.APIKey{Name: "t", Prefix: prefix, KeyHash: hash, AllowedModels: []string{"*"}, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	h.CheckOnce()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"paid","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer "+plain)
	rec := httptest.NewRecorder()
	px.ChatCompletions(rec, req)
	if rec.Code != 200 {
		t.Fatalf("code %d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-MikroLLM-Fallback") == "" {
		t.Fatal("missing fallback header")
	}
	if len(got) != 2 || got[0] != "paid" || got[1] != "cheap" {
		t.Fatalf("upstream models %v", got)
	}
}
