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

func TestSystemOneFromOllama(t *testing.T) {
	st, h, _, px, _ := setup(t)
	var gotPath, gotModel string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.URL.Path == "/api/version" {
			w.Write([]byte(`{"version":"0.35.0"}`))
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotModel, _ = body["model"].(string)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"nimble","answers":{"label":{"type":"choice","choice":"bug"}},"usage":{"input_tokens":12,"output_tokens":1}}`))
	}))
	t.Cleanup(up.Close)
	id, err := st.UpsertBackend("mac", up.URL, true, 1, "ollama", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ConnectOllamaModel("jev", []int64{id}, "least_conn", 8192); err != nil {
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
	body := `{"model":"jev","state":"checkout returns 500","questions":{"label":{"type":"choice","instructions":"label","criteria":{"bug":null}}}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/systemone", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+plain)
	rec := httptest.NewRecorder()
	px.SystemOne(rec, req)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if gotPath != "/v1/systemone" || gotModel != "jev" {
		t.Fatalf("upstream %s model %s", gotPath, gotModel)
	}
	var out struct {
		Model   string `json:"model"`
		Answers map[string]struct {
			Choice string `json:"choice"`
		} `json:"answers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Model != "jev" || out.Answers["label"].Choice != "bug" {
		t.Fatalf("%s", rec.Body.String())
	}

	chat := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"jev","messages":[{"role":"user","content":"hi"}]}`))
	chat.Header.Set("Authorization", "Bearer "+plain)
	crec := httptest.NewRecorder()
	px.ChatCompletions(crec, chat)
	if crec.Code != 400 || !strings.Contains(crec.Body.String(), "/v1/systemone") {
		t.Fatalf("chat %d %s", crec.Code, crec.Body.String())
	}
}

func TestSystemOneOpenRouterPath(t *testing.T) {
	st, h, _, px, _ := setup(t)
	var gotPath string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.URL.Path == "/models" {
			w.Write([]byte(`{"data":[{"id":"jev-latest"}]}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"jev-1.13.0","answers":{"urgent":{"type":"noul","noul":0.9}},"usage":{"input_tokens":3,"output_tokens":1}}`))
	}))
	t.Cleanup(up.Close)
	id, err := st.UpsertBackend("or", up.URL, true, 1, "openrouter", "sk-test")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ConnectOllamaModel("jev-latest", []int64{id}, "least_conn", 0); err != nil {
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
	body := `{"model":"jev-latest","state":{"ticket":"refund"},"questions":{"urgent":{"type":"noul","instructions":"urgent?"}}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/systemone", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+plain)
	rec := httptest.NewRecorder()
	px.SystemOne(rec, req)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if gotPath != "/systemone" {
		t.Fatalf("path %s", gotPath)
	}
	if !strings.Contains(rec.Body.String(), `"model":"jev-latest"`) {
		t.Fatalf("%s", rec.Body.String())
	}
}

func TestSystemOneRejectsHub(t *testing.T) {
	st, _, _, px, _ := setup(t)
	plain, prefix, hash, err := auth.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.InsertKey(store.APIKey{Name: "t", Prefix: prefix, KeyHash: hash, AllowedModels: []string{"*"}, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	body := `{"model":"hub|node|jev-latest","state":"x","questions":{"q":{"type":"noul"}}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/systemone", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+plain)
	rec := httptest.NewRecorder()
	px.SystemOne(rec, req)
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "hub") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

func TestChatRejectsJevWithoutAlias(t *testing.T) {
	_, _, _, px, _ := setup(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"tev1:0.8b","messages":[]}`))
	rec := httptest.NewRecorder()
	px.ServeChat(rec, req)
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "Jev") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}
