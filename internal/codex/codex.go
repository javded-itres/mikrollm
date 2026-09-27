package codex

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// ClientID is the public Codex CLI OAuth client. Refresh uses it; login
	// itself stays in `codex login`, which writes ~/.codex/auth.json.
	ClientID     = "app_EMoamEEZ73f0CkXaXp7hrann"
	DefaultBase  = "https://chatgpt.com/backend-api/codex"
	DefaultModel = "gpt-5.4"
	originator   = "codex_cli_rs"
)

var TokenURL = "https://auth.openai.com/oauth/token"

// Session is the ChatGPT subscription credential stored on the owning node.
type Session struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	AccountID    string `json:"account_id,omitempty"`
	Model        string `json:"model,omitempty"`
}

// Parse accepts either a compact session or Codex auth.json.
func Parse(raw string) (Session, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Session{}, fmt.Errorf("codex auth required")
	}
	var wrap struct {
		Tokens *struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			AccountID    string `json:"account_id"`
			IDToken      any    `json:"id_token"`
		} `json:"tokens"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		AccountID    string `json:"account_id"`
		Model        string `json:"model"`
	}
	if err := json.Unmarshal([]byte(raw), &wrap); err != nil {
		return Session{}, fmt.Errorf("codex auth is not json")
	}
	s := Session{AccessToken: wrap.AccessToken, RefreshToken: wrap.RefreshToken, AccountID: wrap.AccountID, Model: wrap.Model}
	if wrap.Tokens != nil {
		if wrap.Tokens.AccessToken != "" {
			s.AccessToken = wrap.Tokens.AccessToken
		}
		if wrap.Tokens.RefreshToken != "" {
			s.RefreshToken = wrap.Tokens.RefreshToken
		}
		if wrap.Tokens.AccountID != "" {
			s.AccountID = wrap.Tokens.AccountID
		}
		if s.AccountID == "" {
			s.AccountID = accountFromIDToken(wrap.Tokens.IDToken)
		}
	}
	if s.AccountID == "" {
		s.AccountID = accountFromJWT(s.AccessToken)
	}
	if s.AccessToken == "" {
		return Session{}, fmt.Errorf("codex access_token missing")
	}
	if s.Model == "" {
		s.Model = DefaultModel
	}
	return s, nil
}

func (s Session) Compact() string {
	b, _ := json.Marshal(s)
	return string(b)
}

func accountFromIDToken(v any) string {
	switch t := v.(type) {
	case string:
		return accountFromJWT(t)
	default:
		return ""
	}
}

func accountFromJWT(jwt string) string {
	parts := strings.Split(jwt, ".")
	if len(parts) < 2 {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		if raw, err = base64.URLEncoding.DecodeString(parts[1]); err != nil {
			return ""
		}
	}
	var claims struct {
		Auth struct {
			AccountID string `json:"chatgpt_account_id"`
		} `json:"https://api.openai.com/auth"`
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(raw, &claims) != nil {
		return ""
	}
	return claims.Auth.AccountID
}

func jwtExpired(jwt string, skew time.Duration) bool {
	parts := strings.Split(jwt, ".")
	if len(parts) < 2 {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		raw, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return false
		}
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(raw, &claims) != nil || claims.Exp == 0 {
		return false
	}
	return time.Unix(claims.Exp, 0).Add(-skew).Before(time.Now())
}

// Refresh exchanges the refresh token. The caller stores the new compact session.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

func Refresh(ctx context.Context, client Doer, s Session) (Session, error) {
	if s.RefreshToken == "" {
		return s, fmt.Errorf("codex refresh_token missing")
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", s.RefreshToken)
	form.Set("client_id", ClientID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return s, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return s, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return s, fmt.Errorf("codex refresh %s", resp.Status)
	}
	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
	}
	if json.Unmarshal(raw, &out) != nil || out.AccessToken == "" {
		return s, fmt.Errorf("codex refresh: bad token response")
	}
	s.AccessToken = out.AccessToken
	if out.RefreshToken != "" {
		s.RefreshToken = out.RefreshToken
	}
	if id := accountFromJWT(out.IDToken); id != "" {
		s.AccountID = id
	}
	if s.AccountID == "" {
		s.AccountID = accountFromJWT(out.AccessToken)
	}
	return s, nil
}

// Complete turns an OpenAI chat-completions body into one Codex Responses call
// and returns a chat-completions body. stream requests are answered as one SSE chunk.
func Complete(ctx context.Context, client Doer, base string, s Session, chat []byte, persist func(Session)) (int, []byte, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if jwtExpired(s.AccessToken, 2*time.Minute) && s.RefreshToken != "" {
		next, err := Refresh(ctx, client, s)
		if err != nil {
			return 0, nil, err
		}
		s = next
		if persist != nil {
			persist(s)
		}
	}
	reqBody, wantStream, err := toResponses(chat, s.Model)
	if err != nil {
		return 400, nil, err
	}
	status, raw, err := postResponses(ctx, client, base, s, reqBody)
	if err != nil {
		return 0, nil, err
	}
	if status == http.StatusUnauthorized && s.RefreshToken != "" {
		next, rerr := Refresh(ctx, client, s)
		if rerr != nil {
			return status, raw, nil
		}
		s = next
		if persist != nil {
			persist(s)
		}
		status, raw, err = postResponses(ctx, client, base, s, reqBody)
		if err != nil {
			return 0, nil, err
		}
	}
	if status >= 400 {
		return status, raw, nil
	}
	out := toChat(raw, modelOf(chat, s.Model))
	if wantStream {
		return status, toSSE(out), nil
	}
	return status, out, nil
}

func postResponses(ctx context.Context, client Doer, base string, s Session, body []byte) (int, []byte, error) {
	url := strings.TrimRight(base, "/") + "/responses"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.AccessToken)
	req.Header.Set("OpenAI-Beta", "responses=v1")
	req.Header.Set("originator", originator)
	if s.AccountID != "" {
		req.Header.Set("ChatGPT-Account-ID", s.AccountID)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return resp.StatusCode, raw, nil
}

func modelOf(chat []byte, fallback string) string {
	var doc struct {
		Model string `json:"model"`
	}
	if json.Unmarshal(chat, &doc) == nil && strings.TrimSpace(doc.Model) != "" {
		return doc.Model
	}
	if fallback == "" {
		return DefaultModel
	}
	return fallback
}

func toResponses(chat []byte, defaultModel string) ([]byte, bool, error) {
	var doc struct {
		Model    string            `json:"model"`
		Stream   bool              `json:"stream"`
		Tools    []json.RawMessage `json:"tools"`
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if json.Unmarshal(chat, &doc) != nil {
		return nil, false, fmt.Errorf("codex: chat body is not json")
	}
	model := strings.TrimSpace(doc.Model)
	if model == "" || model == "codex" {
		model = defaultModel
	}
	var instructions []string
	var input []any
	for _, m := range doc.Messages {
		text := contentText(m.Content)
		switch m.Role {
		case "system", "developer":
			if text != "" {
				instructions = append(instructions, text)
			}
		case "assistant":
			input = append(input, map[string]any{
				"role":    "assistant",
				"content": []any{map[string]string{"type": "output_text", "text": text}},
			})
		default:
			input = append(input, map[string]any{
				"role":    "user",
				"content": []any{map[string]string{"type": "input_text", "text": text}},
			})
		}
	}
	payload := map[string]any{
		"model":  model,
		"input":  input,
		"store":  false,
		"stream": false,
	}
	if len(instructions) > 0 {
		payload["instructions"] = strings.Join(instructions, "\n\n")
	}
	if len(doc.Tools) > 0 {
		payload["tools"] = chatTools(doc.Tools)
	}
	b, err := json.Marshal(payload)
	return b, doc.Stream, err
}

func chatTools(raw []json.RawMessage) []any {
	var out []any
	for _, item := range raw {
		var tool struct {
			Type     string `json:"type"`
			Function struct {
				Name        string          `json:"name"`
				Description string          `json:"description"`
				Parameters  json.RawMessage `json:"parameters"`
			} `json:"function"`
		}
		if json.Unmarshal(item, &tool) != nil || tool.Function.Name == "" {
			continue
		}
		params := tool.Function.Parameters
		if len(params) == 0 {
			params = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out = append(out, map[string]any{
			"type":        "function",
			"name":        tool.Function.Name,
			"description": tool.Function.Description,
			"parameters":  json.RawMessage(params),
		})
	}
	return out
}

func contentText(raw json.RawMessage) string {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []map[string]any
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var b strings.Builder
	for _, p := range parts {
		if t, ok := p["text"].(string); ok {
			b.WriteString(t)
		}
	}
	return b.String()
}

func toChat(raw []byte, model string) []byte {
	var doc struct {
		Output []struct {
			Type      string `json:"type"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
			CallID    string `json:"call_id"`
			Content   []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	text := ""
	var toolCalls []any
	if json.Unmarshal(raw, &doc) == nil {
		for _, item := range doc.Output {
			if item.Type == "function_call" && item.Name != "" {
				id := item.CallID
				if id == "" {
					id = "call_" + item.Name
				}
				toolCalls = append(toolCalls, map[string]any{
					"id":   id,
					"type": "function",
					"function": map[string]string{
						"name":      item.Name,
						"arguments": item.Arguments,
					},
				})
				continue
			}
			for _, c := range item.Content {
				if c.Text != "" {
					text += c.Text
				}
			}
		}
	}
	msg := map[string]any{"role": "assistant", "content": text}
	finish := "stop"
	if len(toolCalls) > 0 {
		msg["tool_calls"] = toolCalls
		if text == "" {
			msg["content"] = nil
		}
		finish = "tool_calls"
	}
	out := map[string]any{
		"id":      "codex",
		"object":  "chat.completion",
		"model":   model,
		"choices": []any{map[string]any{"index": 0, "message": msg, "finish_reason": finish}},
		"usage": map[string]int{
			"prompt_tokens":     doc.Usage.InputTokens,
			"completion_tokens": doc.Usage.OutputTokens,
			"total_tokens":      doc.Usage.InputTokens + doc.Usage.OutputTokens,
		},
	}
	b, _ := json.Marshal(out)
	return b
}

func toSSE(chat []byte) []byte {
	var doc struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content   any   `json:"content"`
				ToolCalls []any `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	_ = json.Unmarshal(chat, &doc)
	delta := map[string]any{"role": "assistant"}
	if len(doc.Choices) > 0 {
		delta["content"] = doc.Choices[0].Message.Content
		if len(doc.Choices[0].Message.ToolCalls) > 0 {
			delta["tool_calls"] = doc.Choices[0].Message.ToolCalls
		}
	}
	first, _ := json.Marshal(map[string]any{
		"id": doc.ID, "object": "chat.completion.chunk", "model": doc.Model,
		"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}},
	})
	finish := "stop"
	if len(doc.Choices) > 0 && doc.Choices[0].FinishReason != "" {
		finish = doc.Choices[0].FinishReason
	}
	second, _ := json.Marshal(map[string]any{
		"id": doc.ID, "object": "chat.completion.chunk", "model": doc.Model,
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finish}},
	})
	return []byte("data: " + string(first) + "\n\ndata: " + string(second) + "\n\ndata: [DONE]\n\n")
}
