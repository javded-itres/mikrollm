package grok

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	DefaultBase  = "https://cli-chat-proxy.grok.com/v1"
	DefaultModel = "grok-build"
	tokenAuth    = "xai-grok-cli"
	// ClientVersion is what the CLI proxy checks. Without it the reply is
	// "Grok CLI version (none) is outdated".
	ClientVersion = "1.0.41"
	clientName    = "grok-shell"
	signInKey     = "https://accounts.x.ai/sign-in"
)

// Session is the SuperGrok CLI credential stored on the owning node.
type Session struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresAt    string `json:"expires_at,omitempty"`
	Issuer       string `json:"issuer,omitempty"`
	ClientID     string `json:"client_id,omitempty"`
	Model        string `json:"model,omitempty"`
}

// Parse accepts ~/.grok/auth.json or a compact {"access_token","model"} object.
func Parse(raw string) (Session, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Session{}, fmt.Errorf("grok auth required")
	}
	if !strings.HasPrefix(raw, "{") {
		return Session{AccessToken: raw, Model: DefaultModel}, nil
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return Session{}, fmt.Errorf("grok auth is not json")
	}
	s := Session{}
	if v, ok := doc["access_token"]; ok {
		_ = json.Unmarshal(v, &s.AccessToken)
	}
	if v, ok := doc["refresh_token"]; ok {
		_ = json.Unmarshal(v, &s.RefreshToken)
	}
	if v, ok := doc["expires_at"]; ok {
		_ = json.Unmarshal(v, &s.ExpiresAt)
	}
	if v, ok := doc["issuer"]; ok {
		_ = json.Unmarshal(v, &s.Issuer)
	}
	if v, ok := doc["client_id"]; ok {
		_ = json.Unmarshal(v, &s.ClientID)
	}
	if v, ok := doc["model"]; ok {
		_ = json.Unmarshal(v, &s.Model)
	}
	if s.AccessToken == "" {
		s = sessionFromBoxes(doc)
	}
	s.AccessToken = strings.TrimSpace(s.AccessToken)
	if s.AccessToken == "" {
		return Session{}, fmt.Errorf("grok access token missing")
	}
	if s.Model == "" {
		s.Model = DefaultModel
	}
	return s, nil
}

func sessionFromBoxes(doc map[string]json.RawMessage) Session {
	var best Session
	for key, raw := range doc {
		var box struct {
			Key          string `json:"key"`
			RefreshToken string `json:"refresh_token"`
			ExpiresAt    string `json:"expires_at"`
			Issuer       string `json:"oidc_issuer"`
			ClientID     string `json:"oidc_client_id"`
		}
		if json.Unmarshal(raw, &box) != nil || strings.TrimSpace(box.Key) == "" {
			continue
		}
		s := Session{
			AccessToken:  strings.TrimSpace(box.Key),
			RefreshToken: box.RefreshToken,
			ExpiresAt:    box.ExpiresAt,
			Issuer:       box.Issuer,
			ClientID:     box.ClientID,
		}
		if key == signInKey || strings.Contains(key, "accounts.x.ai") {
			return s
		}
		if best.AccessToken == "" {
			best = s
		}
	}
	return best
}

func (s Session) Compact() string {
	b, _ := json.Marshal(s)
	return string(b)
}

type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Complete forwards an OpenAI chat-completions body to the Grok CLI proxy.
// The subscription token never leaves this process.
func expired(s Session) bool {
	if s.ExpiresAt == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, s.ExpiresAt)
	if err != nil {
		return false
	}
	return t.Add(-2 * time.Minute).Before(time.Now())
}

// Refresh uses the OIDC token endpoint advertised by the issuer.
func Refresh(ctx context.Context, client Doer, s Session) (Session, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if s.RefreshToken == "" || s.Issuer == "" || s.ClientID == "" {
		return s, fmt.Errorf("grok refresh token missing")
	}
	discURL := strings.TrimRight(s.Issuer, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discURL, nil)
	if err != nil {
		return s, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return s, err
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return s, fmt.Errorf("grok discovery %s", resp.Status)
	}
	var disc struct {
		TokenEndpoint string `json:"token_endpoint"`
	}
	if json.Unmarshal(raw, &disc) != nil || disc.TokenEndpoint == "" {
		return s, fmt.Errorf("grok discovery: no token endpoint")
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", s.RefreshToken)
	form.Set("client_id", s.ClientID)
	req, err = http.NewRequestWithContext(ctx, http.MethodPost, disc.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return s, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err = client.Do(req)
	if err != nil {
		return s, err
	}
	defer resp.Body.Close()
	raw, _ = io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return s, fmt.Errorf("grok refresh %s", resp.Status)
	}
	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if json.Unmarshal(raw, &out) != nil || out.AccessToken == "" {
		return s, fmt.Errorf("grok refresh: bad token response")
	}
	s.AccessToken = out.AccessToken
	if out.RefreshToken != "" {
		s.RefreshToken = out.RefreshToken
	}
	if out.ExpiresIn > 0 {
		s.ExpiresAt = time.Now().Add(time.Duration(out.ExpiresIn) * time.Second).UTC().Format(time.RFC3339)
	}
	return s, nil
}

func Complete(ctx context.Context, client Doer, base string, s Session, chat []byte, persist func(Session)) (int, []byte, string, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if expired(s) {
		next, err := Refresh(ctx, client, s)
		if err != nil {
			return 0, nil, "", err
		}
		s = next
		if persist != nil {
			persist(s)
		}
	}
	body, model, err := prepare(chat, s.Model)
	if err != nil {
		return 400, nil, "", err
	}
	status, raw, ct, err := postChat(ctx, client, base, s, body, model)
	if err != nil {
		return 0, nil, "", err
	}
	if status == http.StatusUnauthorized {
		next, rerr := Refresh(ctx, client, s)
		if rerr == nil {
			s = next
			if persist != nil {
				persist(s)
			}
			return postChat(ctx, client, base, s, body, model)
		}
	}
	return status, raw, ct, nil
}

func postChat(ctx context.Context, client Doer, base string, s Session, body []byte, model string) (int, []byte, string, error) {
	url := strings.TrimRight(base, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	applyCLIHeaders(req, s)
	req.Header.Set("x-grok-model-override", model)
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/json"
	}
	return resp.StatusCode, raw, ct, nil
}

// Listed is one model the CLI proxy offers on this subscription.
type Listed struct {
	ID      string
	Title   string
	Context int
}

// List reads GET /models. The base URL already includes /v1.
func List(ctx context.Context, client Doer, base string, s Session) ([]Listed, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if expired(s) {
		next, err := Refresh(ctx, client, s)
		if err != nil {
			return nil, err
		}
		s = next
	}
	url := strings.TrimRight(base, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	applyCLIHeaders(req, s)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("grok models %s", resp.Status)
	}
	var doc struct {
		Data []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Context int    `json:"context_window"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return nil, fmt.Errorf("grok models: bad json")
	}
	var out []Listed
	seen := map[string]bool{}
	for _, m := range doc.Data {
		id := strings.TrimSpace(m.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, Listed{ID: id, Title: m.Name, Context: m.Context})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("grok models: empty")
	}
	return out, nil
}

func applyCLIHeaders(req *http.Request, s Session) {
	req.Header.Set("Authorization", "Bearer "+s.AccessToken)
	req.Header.Set("X-XAI-Token-Auth", tokenAuth)
	req.Header.Set("x-grok-client-version", ClientVersion)
	req.Header.Set("x-grok-client-identifier", clientName)
	req.Header.Set("User-Agent", "grok/"+ClientVersion)
}

func prepare(chat []byte, defaultModel string) ([]byte, string, error) {
	if len(bytes.TrimSpace(chat)) == 0 {
		chat = []byte(`{"messages":[]}`)
	}
	var doc map[string]any
	if err := json.Unmarshal(chat, &doc); err != nil {
		return nil, "", fmt.Errorf("grok: chat body is not json")
	}
	model, _ := doc["model"].(string)
	model = strings.TrimSpace(model)
	if model == "" || model == "grok" {
		model = defaultModel
	}
	if model == "" {
		model = DefaultModel
	}
	doc["model"] = model
	out, err := json.Marshal(doc)
	return out, model, err
}
