package hubclient

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/javded-itres/mikrollm/internal/domain"
)

type memStore struct {
	mu     sync.Mutex
	cfg    Settings
	models []domain.Model
}

func (m *memStore) HubSettings() (Settings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg, nil
}

func (m *memStore) SetHubSettings(s Settings) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg = s
	return nil
}

func (m *memStore) ListModels() ([]domain.Model, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]domain.Model, len(m.models))
	copy(out, m.models)
	return out, nil
}

func TestClientRegistersAndAnnounces(t *testing.T) {
	var mu sync.Mutex
	var announced []Alias
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/register":
			_ = json.NewEncoder(w).Encode(RegisterRes{NodeID: "n1", Token: "hk1", Hub: DefaultURL})
		case r.Method == http.MethodPut && r.URL.Path == "/v1/announce":
			var req AnnounceReq
			_ = json.NewDecoder(r.Body).Decode(&req)
			mu.Lock()
			announced = req.Aliases
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/pull":
			w.WriteHeader(http.StatusNoContent)
		default:
			io.Copy(io.Discard, r.Body)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(hs.Close)

	st := &memStore{
		cfg: Settings{Enabled: true, Name: "hap-test"},
		models: []domain.Model{
			{Alias: "coder", Enabled: true, HubShare: true, MaxContext: 8192},
			{Alias: "minimax-hailuo-02", UpstreamName: "minimax-hailuo-02", Enabled: true, HubShare: true},
		},
	}
	c := New(st, nil, ":4000")
	c.HubURL = hs.URL
	c.RelaySecret = "s"
	c.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"local"}`))
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Loop(ctx)
	c.Kick()

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		state, err := c.Status()
		cfg, _ := st.HubSettings()
		mu.Lock()
		n := len(announced)
		mu.Unlock()
		if state == "online" && cfg.Token == "hk1" && cfg.NodeID == "n1" && n == 2 {
			got := map[string]bool{}
			for _, a := range announced {
				got[a.Alias] = true
			}
			if got["coder"] && got["minimax-hailuo-02"] {
				return
			}
		}
		if state == "error" && err != "" {
			t.Log(err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	state, errStr := c.Status()
	t.Fatalf("not online: %s %s cfg=%+v announced=%+v", state, errStr, st.cfg, announced)
}

func TestLocalRelayKinds(t *testing.T) {
	m, p, body := localRelay(Job{Alias: "toy-image", Kind: "images", Body: json.RawMessage(`{"prompt":"x"}`)})
	if m != http.MethodPost || p != "/v1/images/generations" || !bytes.Contains(body, []byte(`"toy-image"`)) {
		t.Fatalf("%s %s %s", m, p, body)
	}
	m, p, body = localRelay(Job{Alias: "hailuo", Kind: "videos_content", Ref: "vid1"})
	if m != http.MethodGet || p != "/v1/videos/vid1/content?model=hailuo" || body != nil {
		t.Fatalf("%s %s %v", m, p, body)
	}
}

func TestCollapseChatSSE(t *testing.T) {
	sse := []byte("data: {\"choices\":[{\"delta\":{\"content\":\"hel\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"lo\"}}]}\n\ndata: [DONE]\n")
	got := CollapseChat(sse)
	if !bytes.Contains(got, []byte(`"hello"`)) {
		t.Fatalf("%s", got)
	}
	reason := []byte(`{"choices":[{"message":{"role":"assistant","content":"","reasoning":"pong"}}]}`)
	c, r := ChatText(reason)
	if c != "" || r != "pong" {
		t.Fatalf("content=%q reasoning=%q", c, r)
	}
}
