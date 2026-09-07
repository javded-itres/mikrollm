package health

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/javded-itres/mikrollm/internal/domain"
)

type staticBackends []domain.Backend

func (s staticBackends) ListBackends() ([]domain.Backend, error) { return s, nil }
func (s staticBackends) GetBackend(id int64) (domain.Backend, error) {
	for _, b := range s {
		if b.ID == id {
			return b, nil
		}
	}
	return domain.Backend{}, http.ErrNoLocation
}

func TestProbeVLLM(t *testing.T) {
	var sawAuth string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(200)
		case "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]string{{"id": "Qwen/Qwen2.5-7B-Instruct"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(up.Close)
	b := domain.Backend{ID: 1, Name: "gpu", BaseURL: up.URL, Kind: "vllm", Token: "abc", Enabled: true}
	c := New(staticBackends{b}, up.Client())
	c.CheckOnce()
	st := c.Get(1)
	if !st.Healthy {
		t.Fatalf("unhealthy: %s", st.Error)
	}
	if len(st.Models) != 1 || st.Models[0] != "Qwen/Qwen2.5-7B-Instruct" {
		t.Fatalf("models %+v", st.Models)
	}
	if len(st.Running) != 1 {
		t.Fatalf("vllm models should count as running %+v", st.Running)
	}
	if sawAuth != "Bearer abc" {
		t.Fatalf("auth %q", sawAuth)
	}
}

func TestProbeLMStudio(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/models" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]any{
				{"key": "ibm/granite-4-micro", "size_bytes": 100, "loaded_instances": []any{}},
				{"key": "qwen/qwen3", "size_bytes": 200, "loaded_instances": []any{map[string]any{"id": "1"}}},
			},
		})
	}))
	t.Cleanup(up.Close)
	b := domain.Backend{ID: 2, Name: "lms", BaseURL: up.URL, Kind: "lmstudio", Enabled: true}
	c := New(staticBackends{b}, up.Client())
	c.CheckOnce()
	st := c.Get(2)
	if !st.Healthy {
		t.Fatalf("unhealthy: %s", st.Error)
	}
	if len(st.Models) != 2 {
		t.Fatalf("models %+v", st.Models)
	}
	if len(st.Running) != 1 || st.Running[0] != "qwen/qwen3" {
		t.Fatalf("running %+v", st.Running)
	}
	if st.Sizes["ibm/granite-4-micro"] != 100 {
		t.Fatalf("sizes %+v", st.Sizes)
	}
}

func TestProbeOpenRouter(t *testing.T) {
	var sawAuth, sawKey, sawModels, sawReferer, sawUA string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		sawReferer = r.Header.Get("HTTP-Referer")
		sawUA = r.Header.Get("User-Agent")
		switch r.URL.Path {
		case "/key":
			sawKey = r.URL.Path
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"label": "test"}})
		case "/models":
			sawModels = r.URL.Path
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "openai/gpt-4o-mini", "name": "OpenAI: GPT-4o Mini", "context_length": 128000,
						"pricing": map[string]any{"prompt": "0.00000015", "completion": "0.0000006"}},
					{"id": "anthropic/claude-sonnet-4", "context_length": 200000,
						"pricing": map[string]any{"prompt": "0", "completion": "0"}},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(up.Close)
	b := domain.Backend{ID: 3, Name: "or", BaseURL: up.URL, Kind: "openrouter", Token: "Bearer sk-or-test", Enabled: true}
	c := New(staticBackends{b}, up.Client())
	c.CheckOnce()
	st := c.Get(3)
	if !st.Healthy {
		t.Fatalf("unhealthy: %s", st.Error)
	}
	if sawKey != "/key" || sawModels != "/models" || sawAuth != "Bearer sk-or-test" || sawReferer == "" || sawUA == "" {
		t.Fatalf("key %s models %s auth %s referer %s ua %s", sawKey, sawModels, sawAuth, sawReferer, sawUA)
	}
	if len(st.Models) != 2 || st.Models[0] != "openai/gpt-4o-mini" {
		t.Fatalf("models %+v", st.Models)
	}
	if st.Contexts["openai/gpt-4o-mini"] != 128000 || st.Contexts["anthropic/claude-sonnet-4"] != 200000 {
		t.Fatalf("contexts %+v", st.Contexts)
	}
	if st.Providers["openai/gpt-4o-mini"] != "OpenAI" {
		t.Fatalf("provider %+v", st.Providers)
	}
	if !st.Priced["openai/gpt-4o-mini"] || st.Prompt["openai/gpt-4o-mini"] != 0.15 {
		t.Fatalf("price prompt=%v priced=%v", st.Prompt["openai/gpt-4o-mini"], st.Priced)
	}
	cat := c.Catalog([]domain.Backend{b})
	if len(cat) != 2 || cat[0].Provider == "" || !cat[1].Priced {
		t.Fatalf("catalog %+v", cat)
	}
}

func TestProbeOpenRouterForbidden(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"your IP address is not in the allowlist for this account","code":403}}`))
	}))
	t.Cleanup(up.Close)
	b := domain.Backend{ID: 6, Name: "or", BaseURL: up.URL, Kind: "openrouter", Token: "sk-or-test", Enabled: true}
	c := New(staticBackends{b}, up.Client())
	c.CheckOnce()
	st := c.Get(6)
	if st.Healthy {
		t.Fatal("expected unhealthy")
	}
	if st.Error == "" || st.Error == "403 Forbidden" {
		t.Fatalf("want parsed 403, got %q", st.Error)
	}
}

func TestProbeOpenRouterNeedsKey(t *testing.T) {
	b := domain.Backend{ID: 4, Name: "or", BaseURL: "http://127.0.0.1:9", Kind: "openrouter", Enabled: true}
	c := New(staticBackends{b}, http.DefaultClient)
	c.CheckOnce()
	st := c.Get(4)
	if st.Healthy || st.Error == "" {
		t.Fatalf("%+v", st)
	}
}

func TestProbeOllamaCloud(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ol-key" {
			w.WriteHeader(401)
			return
		}
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]any{{"name": "gpt-oss:120b", "size": 0}},
		})
	}))
	t.Cleanup(up.Close)
	b := domain.Backend{ID: 5, Name: "oc", BaseURL: up.URL, Kind: "ollama-cloud", Token: "ol-key", Enabled: true}
	c := New(staticBackends{b}, up.Client())
	c.CheckOnce()
	st := c.Get(5)
	if !st.Healthy {
		t.Fatalf("unhealthy: %s", st.Error)
	}
	if len(st.Models) != 1 || st.Models[0] != "gpt-oss:120b" {
		t.Fatalf("models %+v", st.Models)
	}
	if len(st.Running) != 0 {
		t.Fatalf("cloud must not report local RAM %+v", st.Running)
	}
}
