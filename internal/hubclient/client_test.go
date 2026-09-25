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

func (m *memStore) UpsertHubAuto(nodeID, nodeName, upstream string, maxContext int, media []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.models {
		if m.models[i].Alias == domain.HubAutoAlias {
			m.models[i].HubNodeID = nodeID
			m.models[i].HubNodeName = nodeName
			m.models[i].UpstreamName = upstream
			m.models[i].MaxContext = maxContext
			m.models[i].Media = media
			m.models[i].Enabled = true
			m.models[i].HubShare = false
			return nil
		}
	}
	m.models = append(m.models, domain.Model{
		Alias: domain.HubAutoAlias, UpstreamName: upstream, Enabled: true,
		HubNodeID: nodeID, HubNodeName: nodeName, MaxContext: maxContext, Media: media,
	})
	return nil
}

func (m *memStore) DeleteHubAuto() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := m.models[:0]
	for _, x := range m.models {
		if x.Alias == domain.HubAutoAlias && x.HubNodeID != "" {
			continue
		}
		out = append(out, x)
	}
	m.models = out
	return nil
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

func TestClientSyncsAuto(t *testing.T) {
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v1/catalog" {
			_ = json.NewEncoder(w).Encode(Catalog{
				Online: 1, Total: 1,
				Defaults: &Defaults{NodeID: "npeer", NodeName: "ams-1", Alias: "coder"},
				Nodes: []NodePublic{{
					ID: "npeer", Name: "ams-1", Online: true,
					Aliases: []Alias{{Alias: "coder", Media: []string{"chat"}, Context: 8192}},
				}},
			})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(hs.Close)
	st := &memStore{cfg: Settings{Enabled: true, Name: "hap-test", NodeID: "nself", Token: "hk"}}
	c := New(st, nil, ":4000")
	c.HubURL = hs.URL
	c.RefreshCatalog()
	ms, _ := st.ListModels()
	if len(ms) != 1 || ms[0].Alias != "auto" || ms[0].HubNodeID != "npeer" || ms[0].UpstreamName != "coder" {
		t.Fatalf("auto %+v", ms)
	}
	st.cfg.Enabled = false
	c.RefreshCatalog()
	ms, _ = st.ListModels()
	if len(ms) != 0 {
		t.Fatalf("auto lingered %+v", ms)
	}
}

func TestClientSyncsAutoPool(t *testing.T) {
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v1/catalog" {
			_ = json.NewEncoder(w).Encode(Catalog{
				Defaults: &Defaults{
					NodeID: "nfast", Alias: "small",
					Pool: []PoolEntry{
						{NodeID: "nfast", Alias: "small", Tier: "fast", Context: 4096, Media: []string{"chat"}},
						{NodeID: "nbig", Alias: "coder", Tier: "strong", Context: 32768, Media: []string{"chat"}},
					},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(hs.Close)
	st := &memStore{cfg: Settings{Enabled: true, Name: "hap-test", NodeID: "nself", Token: "hk"}}
	c := New(st, nil, ":4000")
	c.HubURL = hs.URL
	c.RefreshCatalog()
	ms, _ := st.ListModels()
	if len(ms) != 1 || ms[0].HubNodeID != domain.HubAutoRouter || ms[0].UpstreamName != "auto" || ms[0].MaxContext != 32768 {
		t.Fatalf("pool auto %+v", ms)
	}
	if len(ms[0].Media) != 1 || ms[0].Media[0] != domain.MediaChat {
		t.Fatalf("media %+v", ms[0].Media)
	}
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

func TestRunJobPreservesToolCalls(t *testing.T) {
	var got Result
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/result" {
			_ = json.NewDecoder(r.Body).Decode(&got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(hs.Close)

	st := &memStore{models: []domain.Model{
		{Alias: "coder", Enabled: true, HubShare: true},
	}}
	c := New(st, nil, ":4000")
	c.HubURL = hs.URL
	c.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-7","model":"kimi","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":"","reasoning":"call it","tool_calls":[{"id":"call_1","index":0,"type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Moscow\"}"}}]}}],"usage":{"prompt_tokens":9,"completion_tokens":3,"total_tokens":12}}`))
	})

	job := Job{ID: "j1", Alias: "coder", Body: json.RawMessage(`{"model":"coder","messages":[{"role":"user","content":"weather?"}],"tools":[{"type":"function","function":{"name":"get_weather","parameters":{"type":"object"}}}]}`)}
	c.runJob(Settings{}, job)

	if got.JobID != "j1" || got.Status != 200 {
		t.Fatalf("result not posted: %+v", got)
	}
	collapsed := got.Body
	if !bytes.Contains(collapsed, []byte(`"get_weather"`)) ||
		!bytes.Contains(collapsed, []byte(`"arguments":"{\"city\":\"Moscow\"}"`)) ||
		!bytes.Contains(collapsed, []byte(`"finish_reason":"tool_calls"`)) ||
		!bytes.Contains(collapsed, []byte(`"total_tokens":12`)) {
		t.Fatalf("tool call data lost in relay: %s", collapsed)
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				ToolCalls []ToolCall `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(collapsed, &parsed) != nil || len(parsed.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("collapsed body not parseable: %s", collapsed)
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
