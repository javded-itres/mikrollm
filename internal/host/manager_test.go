package host

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/javded-itres/mikrollm/internal/domain"
)

func TestCloudRejectsPull(t *testing.T) {
	m := New(http.DefaultClient)
	for _, kind := range []string{"openrouter", "ollama-cloud"} {
		b := domain.Backend{Kind: kind, BaseURL: "https://example.invalid"}
		if err := m.Pull(context.Background(), b, "x", io.Discard); err == nil {
			t.Fatalf("%s pull should fail", kind)
		}
		if err := m.Load(context.Background(), b, "x"); err == nil {
			t.Fatalf("%s load should fail", kind)
		}
	}
}

func TestVLLMLoadReturnsHelp(t *testing.T) {
	m := New(http.DefaultClient)
	b := domain.Backend{Kind: "vllm", BaseURL: "http://127.0.0.1:8000"}
	err := m.Load(context.Background(), b, "Qwen/Qwen2.5-7B-Instruct")
	if err == nil || !strings.Contains(err.Error(), "vllm serve") {
		t.Fatalf("got %v", err)
	}
	if err := m.Pull(context.Background(), b, "x", io.Discard); err == nil {
		t.Fatal("pull should fail")
	}
	if err := m.Unload(context.Background(), b, "x"); err == nil {
		t.Fatal("unload should fail")
	}
}

func TestLMStudioLoadAuth(t *testing.T) {
	var gotPath, gotAuth, gotBody string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"status":"loaded"}`))
	}))
	t.Cleanup(up.Close)
	m := New(up.Client())
	b := domain.Backend{Kind: "lmstudio", BaseURL: up.URL, Token: "lms-tok"}
	if err := m.Load(context.Background(), b, "ibm/granite-4-micro"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/models/load" {
		t.Fatalf("path %s", gotPath)
	}
	if gotAuth != "Bearer lms-tok" {
		t.Fatalf("auth %s", gotAuth)
	}
	var payload map[string]any
	if json.Unmarshal([]byte(gotBody), &payload) != nil || payload["model"] != "ibm/granite-4-micro" {
		t.Fatalf("body %s", gotBody)
	}
}

func TestLMStudioDownloadAlreadyThere(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/models/download" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "already_downloaded"})
	}))
	t.Cleanup(up.Close)
	m := New(up.Client())
	b := domain.Backend{Kind: "lmstudio", BaseURL: up.URL}
	var buf bytes.Buffer
	if err := m.Pull(context.Background(), b, "ibm/granite-4-micro", &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "success") {
		t.Fatalf("log %s", buf.String())
	}
}