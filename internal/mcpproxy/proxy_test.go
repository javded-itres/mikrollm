package mcpproxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/javded-itres/mikrollm/internal/domain"
)

type memStore struct {
	up  domain.MCPUpstream
	hub domain.HubSettings
}

func (m memStore) GetMCPByName(name string) (domain.MCPUpstream, error) {
	if m.up.Name != name {
		return domain.MCPUpstream{}, errMissing
	}
	return m.up, nil
}

func (m memStore) HubSettings() (domain.HubSettings, error) { return m.hub, nil }

type missingError struct{}

func (missingError) Error() string { return "missing" }

var errMissing error = missingError{}

func TestLocalProxyHidesUpstreamToken(t *testing.T) {
	var gotAuth string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.Header.Get("Mcp-Protocol-Version") != "2025-06-18" {
			t.Errorf("protocol %q", r.Header.Get("Mcp-Protocol-Version"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Mcp-Session-Id", "sess")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`))
	}))
	t.Cleanup(up.Close)
	p := New(memStore{up: domain.MCPUpstream{Name: "files", URL: up.URL, Token: "upstream-secret", Enabled: true}}, func(http.ResponseWriter, *http.Request) bool {
		return true
	}, func() string { return "" })
	mux := http.NewServeMux()
	p.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, "/mcp/u/files", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	req.Header.Set("Authorization", "Bearer mcp-client")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Mcp-Protocol-Version", "2025-06-18")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if gotAuth != "Bearer upstream-secret" {
		t.Fatalf("upstream auth %q", gotAuth)
	}
	if rec.Header().Get("Mcp-Session-Id") != "sess" {
		t.Fatalf("session %q", rec.Header().Get("Mcp-Session-Id"))
	}
}

func TestLocalStreamFlushesFirstEvent(t *testing.T) {
	release := make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Mcp-Session-Id", "sess")
		_, _ = w.Write([]byte("data: {\"n\":1}\n\n"))
		w.(http.Flusher).Flush()
		<-release
		_, _ = w.Write([]byte("data: {\"n\":2}\n\n"))
	}))
	t.Cleanup(up.Close)
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	p := New(memStore{up: domain.MCPUpstream{Name: "files", URL: up.URL, Token: "upstream-secret", Enabled: true}}, func(http.ResponseWriter, *http.Request) bool {
		return true
	}, func() string { return "" })
	mux := http.NewServeMux()
	p.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, "/mcp/u/files", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	req.Header.Set("Accept", "text/event-stream")
	rec := &flushRec{ResponseRecorder: httptest.NewRecorder(), saw: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		mux.ServeHTTP(rec, req)
		close(done)
	}()
	select {
	case <-rec.saw:
	case <-done:
		t.Fatalf("ended before flush: %s", rec.Body.String())
	case <-time.After(3 * time.Second):
		t.Fatal("timed out")
	}
	if strings.Contains(rec.Body.String(), `"n":2`) || rec.Header().Get("Mcp-Session-Id") != "sess" || rec.Header().Get("X-Accel-Buffering") != "no" {
		t.Fatalf("headers %v body %s", rec.Header(), rec.Body.String())
	}
	close(release)
	<-done
	if !strings.Contains(rec.Body.String(), `"n":2`) {
		t.Fatalf("body %s", rec.Body.String())
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

func TestRemoteProxyRelaysEnvelope(t *testing.T) {
	var path, authz string
	var body []byte
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		authz = r.Header.Get("Authorization")
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Mcp-Session-Id", "from-peer")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`))
	}))
	t.Cleanup(hub.Close)
	p := New(memStore{hub: domain.HubSettings{Enabled: true}}, func(http.ResponseWriter, *http.Request) bool { return true }, func() string { return hub.URL })
	mux := http.NewServeMux()
	p.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, "/mcp/u/n12345678/files", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Authorization", "Bearer mcp-client")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Header().Get("Mcp-Session-Id") != "from-peer" {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if path != "/v1/relay/n12345678/mcp/files" || authz != "" || !strings.Contains(string(body), `"tools/list"`) || strings.Contains(string(body), "mcp-client") {
		t.Fatalf("path=%s auth=%q body=%s", path, authz, body)
	}
}

func TestRemoteRequiresHubMembership(t *testing.T) {
	p := New(memStore{}, func(http.ResponseWriter, *http.Request) bool { return true }, func() string { return "http://hub" })
	mux := http.NewServeMux()
	p.Mount(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp/u/n12345678/files", strings.NewReader(`{}`)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("%d", rec.Code)
	}
}
