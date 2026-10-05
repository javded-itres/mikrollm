package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/javded-itres/mikrollm/internal/auth"
	"github.com/javded-itres/mikrollm/internal/store"
)

func TestEmbeddingsFromOllama(t *testing.T) {
	st, h, _, px, _ := setup(t)
	var gotPath, gotModel string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.URL.Path == "/api/version" {
			w.Write([]byte(`{"version":"0.34.2"}`))
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotModel, _ = body["model"].(string)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"s1gnature/laya","embeddings":[[0.25,0.5]],"prompt_eval_count":4}`))
	}))
	t.Cleanup(up.Close)
	id, err := st.UpsertBackend("mac", up.URL, true, 1, "ollama", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ConnectOllamaModel("laya", []int64{id}, "least_conn", 8192); err != nil {
		t.Fatal(err)
	}
	h.CheckOnce()
	plain, prefix, hash, err := auth.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.InsertKey(store.APIKey{Name: "t", Prefix: prefix, KeyHash: hash, AllowedModels: []string{"*"}, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader(`{"model":"laya","input":"ping"}`))
	req.Header.Set("Authorization", "Bearer "+plain)
	rec := httptest.NewRecorder()
	px.Embeddings(rec, req)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if gotPath != "/api/embed" || gotModel != "laya" {
		t.Fatalf("upstream %s model %s", gotPath, gotModel)
	}
	var out struct {
		Object string `json:"object"`
		Data   []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
		Usage struct {
			PromptTokens int `json:"prompt_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Object != "list" || len(out.Data) != 1 || len(out.Data[0].Embedding) != 2 || out.Data[0].Embedding[0] != 0.25 || out.Usage.PromptTokens != 4 {
		t.Fatalf("%s", rec.Body.String())
	}

	chat := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"laya","messages":[{"role":"user","content":"hi"}]}`))
	chat.Header.Set("Authorization", "Bearer "+plain)
	crec := httptest.NewRecorder()
	px.ChatCompletions(crec, chat)
	if crec.Code != 400 || !strings.Contains(crec.Body.String(), "/v1/embeddings") {
		t.Fatalf("chat %d %s", crec.Code, crec.Body.String())
	}
}

func TestChatRejectsLayaWithoutAlias(t *testing.T) {
	_, _, _, px, _ := setup(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"s1gnature/laya","messages":[]}`))
	rec := httptest.NewRecorder()
	px.ServeChat(rec, req)
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "эмбеддинг") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}
