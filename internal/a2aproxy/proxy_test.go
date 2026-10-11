package a2aproxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

func TestFetchCardUsesProfileWithoutURL(t *testing.T) {
	rep, err := FetchCard(context.Background(), domain.A2AUpstream{
		Name: "shop", DisplayName: "Магазин", Description: "товары", Company: "Shop",
		Skills: []domain.A2ASkill{{ID: "goods", Name: "Товары", Description: "каталог"}},
	})
	if err != nil || rep.Status != 200 {
		t.Fatal(err, rep.Status)
	}
	body := string(rep.Body)
	for _, want := range []string{`"name":"Магазин"`, `"id":"goods"`, `"organization":"Shop"`, "каталог"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s in %s", want, body)
		}
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

func TestLocalStreamFlushesFirstEvent(t *testing.T) {
	first := make(chan struct{})
	release := make(chan struct{})
	var gotAccept string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"n\":1}\n\n"))
		w.(http.Flusher).Flush()
		close(first)
		<-release
		_, _ = w.Write([]byte("data: {\"n\":2}\n\n"))
		w.(http.Flusher).Flush()
	}))
	t.Cleanup(up.Close)
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	p := New(memStore{up: domain.A2AUpstream{Name: "planner", URL: up.URL + "/rpc", Token: "agent-secret", Enabled: true}}, func(http.ResponseWriter, *http.Request) bool {
		return true
	}, func() string { return "" })
	mux := http.NewServeMux()
	p.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, "/a2a/u/planner", strings.NewReader(`{"jsonrpc":"2.0","id":"1","method":"message/stream"}`))
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	rec := &flushRec{ResponseRecorder: httptest.NewRecorder(), saw: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		mux.ServeHTTP(rec, req)
		close(done)
	}()
	select {
	case <-rec.saw:
	case <-done:
		t.Fatalf("handler ended before the first flush: %s", rec.Body.String())
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for the first event")
	}
	if !strings.Contains(rec.Body.String(), `"n":1`) || strings.Contains(rec.Body.String(), `"n":2`) {
		t.Fatalf("first event was not flushed alone: %s", rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "text/event-stream" || rec.Header().Get("X-Accel-Buffering") != "no" {
		t.Fatalf("headers %v", rec.Header())
	}
	close(release)
	<-done
	if !strings.Contains(rec.Body.String(), `"n":2`) || gotAccept != "text/event-stream" {
		t.Fatalf("accept %q body %s", gotAccept, rec.Body.String())
	}
}

type flushRec struct {
	*httptest.ResponseRecorder
	saw chan struct{}
}

func (f *flushRec) Flush() {
	f.ResponseRecorder.Flush()
	select {
	case <-f.saw:
	default:
		if strings.Contains(f.Body.String(), `"n":1`) {
			close(f.saw)
		}
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
