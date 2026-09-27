package grok

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseIssuerBox(t *testing.T) {
	raw := `{"https://auth.x.ai::abc":{"key":"sess-2","refresh_token":"r","oidc_issuer":"https://auth.x.ai","oidc_client_id":"cid","expires_at":"2026-10-01T00:00:00Z"}}`
	s, err := Parse(raw)
	if err != nil || s.AccessToken != "sess-2" || s.RefreshToken != "r" || s.ClientID != "cid" || s.Issuer != "https://auth.x.ai" {
		t.Fatalf("%+v %v", s, err)
	}
}

func TestParseAuthJSON(t *testing.T) {
	raw := `{"https://accounts.x.ai/sign-in":{"key":"sess-1"}}`
	s, err := Parse(raw)
	if err != nil || s.AccessToken != "sess-1" || s.Model != DefaultModel {
		t.Fatalf("%+v %v", s, err)
	}
	again, err := Parse(s.Compact())
	if err != nil || again.AccessToken != "sess-1" {
		t.Fatal(err)
	}
}

func TestListModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" || r.Header.Get("X-XAI-Token-Auth") != "xai-grok-cli" || r.Header.Get("x-grok-client-version") == "" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"grok-4.7","name":"Grok 4.7","context_window":500000},{"id":"grok-4.7"}]}`))
	}))
	defer srv.Close()
	list, err := List(context.Background(), srv.Client(), srv.URL, Session{AccessToken: "s"})
	if err != nil || len(list) != 1 || list[0].ID != "grok-4.7" || list[0].Context != 500000 {
		t.Fatalf("%+v %v", list, err)
	}
}

func TestCompleteSetsCLIHeaders(t *testing.T) {
	var auth, tokenAuth, override, model, version string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path %s", r.URL.Path)
		}
		auth = r.Header.Get("Authorization")
		tokenAuth = r.Header.Get("X-XAI-Token-Auth")
		override = r.Header.Get("x-grok-model-override")
		version = r.Header.Get("x-grok-client-version")
		b, _ := io.ReadAll(r.Body)
		if strings.Contains(string(b), `"model":"grok-build"`) {
			model = "grok-build"
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer srv.Close()
	status, body, ct, err := Complete(context.Background(), srv.Client(), srv.URL, Session{AccessToken: "sess-1", Model: DefaultModel}, []byte(`{"model":"grok","messages":[{"role":"user","content":"hi"}]}`), nil)
	if err != nil || status != 200 || !strings.Contains(string(body), "ok") {
		t.Fatalf("%d %v %s", status, err, body)
	}
	if auth != "Bearer sess-1" || tokenAuth != "xai-grok-cli" || override != DefaultModel || model != DefaultModel || version != ClientVersion {
		t.Fatalf("auth %s token %s override %s model %s version %s", auth, tokenAuth, override, model, version)
	}
	if !strings.Contains(ct, "json") {
		t.Fatal(ct)
	}
}
