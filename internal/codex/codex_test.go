package codex

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseAuthJSON(t *testing.T) {
	raw := `{"tokens":{"access_token":"aaa","refresh_token":"rrr","account_id":"acc"}}`
	s, err := Parse(raw)
	if err != nil || s.AccessToken != "aaa" || s.RefreshToken != "rrr" || s.AccountID != "acc" || s.Model != DefaultModel {
		t.Fatalf("%+v %v", s, err)
	}
	again, err := Parse(s.Compact())
	if err != nil || again.AccessToken != "aaa" {
		t.Fatal(err)
	}
}

func TestCompleteTranslatesChat(t *testing.T) {
	var sawAuth, sawAccount, sawModel string
	var sawTools bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Errorf("path %s", r.URL.Path)
		}
		sawAuth = r.Header.Get("Authorization")
		sawAccount = r.Header.Get("ChatGPT-Account-ID")
		var body struct {
			Model string            `json:"model"`
			Tools []json.RawMessage `json:"tools"`
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		sawModel = body.Model
		sawTools = len(body.Tools) == 1
		_, _ = w.Write([]byte(`{"output":[{"type":"message","content":[{"type":"output_text","text":"pong"}]},{"type":"function_call","name":"ping","arguments":"{}","call_id":"c1"}],"usage":{"input_tokens":3,"output_tokens":4}}`))
	}))
	defer srv.Close()
	sess := Session{AccessToken: "aaa", AccountID: "acc", Model: DefaultModel}
	chat := []byte(`{"model":"codex","stream":false,"messages":[{"role":"system","content":"be brief"},{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"ping","parameters":{"type":"object"}}}]}`)
	status, body, err := Complete(context.Background(), srv.Client(), srv.URL, sess, chat, nil)
	if err != nil || status != 200 {
		t.Fatalf("%d %v %s", status, err, body)
	}
	if sawAuth != "Bearer aaa" || sawAccount != "acc" || sawModel != DefaultModel || !sawTools {
		t.Fatalf("auth %s account %s model %s tools %v", sawAuth, sawAccount, sawModel, sawTools)
	}
	var out struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					Function struct {
						Name string `json:"name"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens int `json:"prompt_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(body, &out) != nil || len(out.Choices) != 1 {
		t.Fatalf("%s", body)
	}
	if out.Choices[0].Message.Content != "pong" || out.Choices[0].FinishReason != "tool_calls" || out.Choices[0].Message.ToolCalls[0].Function.Name != "ping" {
		t.Fatalf("%+v", out.Choices[0])
	}
	if out.Usage.PromptTokens != 3 {
		t.Fatalf("usage %+v", out.Usage)
	}
}

func TestCompleteRefreshesOn401(t *testing.T) {
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "oauth/token") {
			_, _ = w.Write([]byte(`{"access_token":"new","refresh_token":"r2"}`))
			return
		}
		n++
		if r.Header.Get("Authorization") == "Bearer old" {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"error":"expired"}`))
			return
		}
		_, _ = w.Write([]byte(`{"output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}`))
	}))
	defer srv.Close()
	old := TokenURL
	TokenURL = srv.URL + "/oauth/token"
	t.Cleanup(func() { TokenURL = old })
	var saved Session
	sess := Session{AccessToken: "old", RefreshToken: "r", AccountID: "a", Model: DefaultModel}
	status, body, err := Complete(context.Background(), srv.Client(), srv.URL, sess, []byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hi"}]}`), func(s Session) { saved = s })
	if err != nil || status != 200 || !strings.Contains(string(body), "ok") || saved.AccessToken != "new" || n < 2 {
		t.Fatalf("status %d err %v saved %+v n %d body %s", status, err, saved, n, body)
	}
}
