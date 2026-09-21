package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPullDeleteUnload(t *testing.T) {
	var pulled, deleted, unloaded, loaded string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		name, _ := m["model"].(string)
		switch {
		case r.URL.Path == "/api/pull" && r.Method == http.MethodPost:
			pulled = name
			_, _ = io.WriteString(w, "{\"status\":\"success\"}\n")
		case r.URL.Path == "/api/delete":
			deleted = name
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/api/generate":
			ka, _ := m["keep_alive"].(float64)
			if ka < 0 {
				loaded = name
			} else {
				unloaded = name
			}
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	c := New(nil)
	var buf bytes.Buffer
	if err := c.Pull(context.Background(), srv.URL, "qwen:latest", &buf); err != nil {
		t.Fatal(err)
	}
	if pulled != "qwen:latest" || !strings.Contains(buf.String(), "success") {
		t.Fatalf("pull %q %s", pulled, buf.String())
	}
	if err := c.Delete(context.Background(), srv.URL, "qwen:latest"); err != nil {
		t.Fatal(err)
	}
	if deleted != "qwen:latest" {
		t.Fatalf("delete %q", deleted)
	}
	if err := c.Unload(context.Background(), srv.URL, "qwen:latest"); err != nil {
		t.Fatal(err)
	}
	if unloaded != "qwen:latest" {
		t.Fatalf("unload %q", unloaded)
	}
	if err := c.Load(context.Background(), srv.URL, "qwen:latest", 0); err != nil {
		t.Fatal(err)
	}
	if loaded != "qwen:latest" {
		t.Fatalf("load %q", loaded)
	}
}

func TestLoadWithNumCtx(t *testing.T) {
	var got map[string]any
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		w.WriteHeader(200)
	}))
	t.Cleanup(up.Close)
	c := New(up.Client())
	if err := c.Load(context.Background(), up.URL, "qwen3:32b", 65536); err != nil {
		t.Fatal(err)
	}
	opts, ok := got["options"].(map[string]any)
	if !ok || opts["num_ctx"] != float64(65536) {
		t.Fatalf("num_ctx not sent: %s", got)
	}
	if got["keep_alive"] != float64(-1) {
		t.Fatalf("keep_alive: %v", got["keep_alive"])
	}
	// 0 = no num_ctx key
	got = nil
	if err := c.Load(context.Background(), up.URL, "qwen3:32b", 0); err != nil {
		t.Fatal(err)
	}
	if _, has := got["options"].(map[string]any)["num_ctx"]; has {
		t.Fatalf("num_ctx should be omitted when 0: %v", got["options"])
	}
}
