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
	bid, err := st.UpsertBackend("mac", up.URL, true, 1)
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
	bid, err := st.UpsertBackend("mac", up.URL, true, 1)
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
