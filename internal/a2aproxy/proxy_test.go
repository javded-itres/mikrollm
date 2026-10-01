package a2aproxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/javded-itres/mikrollm/internal/domain"
)

type memStore struct {
	up  domain.A2AUpstream
	hub domain.HubSettings
}

func (m memStore) GetA2AByName(name string) (domain.A2AUpstream, error) {
	if m.up.Name != name {
		return domain.A2AUpstream{}, errMissing
	}
	return m.up, nil
}

func (m memStore) HubSettings() (domain.HubSettings, error) { return m.hub, nil }

type missingError struct{}

func (missingError) Error() string { return "missing" }

var errMissing error = missingError{}

const peerNode = "n0123456789abcdef0123456789abcdef"

func TestLocalCardRewritesURL(t *testing.T) {
	var gotAuth, gotPath string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		if r.Header.Get("Authorization") == "Bearer mcp-client" {
			t.Error("client token forwarded")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"planner","url":"http://10.1.1.9/a2a","protocolVersion":"0.3.0","additionalInterfaces":[{"url":"http://10.1.1.9/a2a"}]}`))
	}))
	t.Cleanup(up.Close)
	p := New(memStore{up: domain.A2AUpstream{Name: "planner", URL: up.URL + "/rpc", Token: "agent-secret", Enabled: true}}, func(http.ResponseWriter, *http.Request) bool {
		return true
	}, func() string { return "" })
	mux := http.NewServeMux()
	p.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/a2a/u/planner/.well-known/agent-card.json", nil)
	req.Host = "gw.example"
	req.Header.Set("Authorization", "Bearer mcp-client")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, `"url":"http://gw.example/a2a/u/planner"`) {
		t.Fatalf("%d %s", rec.Code, body)
	}
	if strings.Contains(body, "10.1.1.9") || strings.Contains(body, "agent-secret") {
		t.Fatalf("upstream leaked %s", body)
	}
	if gotAuth != "Bearer agent-secret" || gotPath != "/.well-known/agent-card.json" {
		t.Fatalf("upstream auth %q path %q", gotAuth, gotPath)
	}
}

func TestLocalPostUsesRPCURL(t *testing.T) {
	var gotAuth, gotPath, gotBody string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":{"id":"task"}}`))
	}))
	t.Cleanup(up.Close)
	p := New(memStore{up: domain.A2AUpstream{Name: "planner", URL: up.URL + "/rpc", Token: "agent-secret", Enabled: true}}, func(http.ResponseWriter, *http.Request) bool {
		return true
	}, func() string { return "" })
	mux := http.NewServeMux()
	p.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, "/a2a/u/planner", strings.NewReader(`{"jsonrpc":"2.0","method":"message/send"}`))
	req.Header.Set("Authorization", "Bearer mcp-client")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"task"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if gotAuth != "Bearer agent-secret" || gotPath != "/rpc" || !strings.Contains(gotBody, "message/send") || strings.Contains(gotBody, "mcp-client") {
		t.Fatalf("auth %q path %q body %s", gotAuth, gotPath, gotBody)
	}
}

func TestRemoteRelaysAndRewritesCard(t *testing.T) {
	var path, authz string
	var body []byte
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		authz = r.Header.Get("Authorization")
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"planner","url":"http://10.1.1.9/a2a"}`))
	}))
	t.Cleanup(hub.Close)
	p := New(memStore{hub: domain.HubSettings{Enabled: true}}, func(http.ResponseWriter, *http.Request) bool { return true }, func() string { return hub.URL })
	mux := http.NewServeMux()
	p.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/a2a/u/"+peerNode+"/planner/.well-known/agent-card.json", nil)
	req.Host = "gw.example"
	req.Header.Set("Authorization", "Bearer mcp-client")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	out := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(out, `"url":"http://gw.example/a2a/u/`+peerNode+`/planner"`) || strings.Contains(out, "10.1.1.9") {
		t.Fatalf("%d %s", rec.Code, out)
	}
	if path != "/v1/relay/"+peerNode+"/a2a/planner" || authz != "" || !strings.Contains(string(body), ".well-known/agent-card.json") || strings.Contains(string(body), "mcp-client") {
		t.Fatalf("path=%s auth=%q body=%s", path, authz, body)
	}
}

func TestRemoteRequiresHubMembership(t *testing.T) {
	p := New(memStore{}, func(http.ResponseWriter, *http.Request) bool { return true }, func() string { return "http://hub" })
	mux := http.NewServeMux()
	p.Mount(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/a2a/u/"+peerNode+"/planner/.well-known/agent-card.json", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

func TestJoinRejectsEscape(t *testing.T) {
	if _, ok := Join("http://127.0.0.1/rpc", "../etc"); ok {
		t.Fatal("dotdot accepted")
	}
	if _, ok := Join("http://127.0.0.1/rpc", "http://evil"); ok {
		t.Fatal("absolute accepted")
	}
	got, ok := Join("http://127.0.0.1/rpc/", "")
	if !ok || got != "http://127.0.0.1/rpc" {
		t.Fatalf("%q %v", got, ok)
	}
}
