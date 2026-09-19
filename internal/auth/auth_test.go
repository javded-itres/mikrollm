package auth

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/javded-itres/mikrollm/internal/domain"
)

type stubStore struct {
	secret  string
	mcpHash string
	admin   string
}

func (s *stubStore) GetKeyByHash(string) (domain.APIKey, error) {
	return domain.APIKey{}, errAuth
}
func (s *stubStore) AdminHash() (string, error)     { return s.admin, nil }
func (s *stubStore) SessionSecret() (string, error) { return s.secret, nil }
func (s *stubStore) MCPTokenHash() (string, error)  { return s.mcpHash, nil }

func TestClientIPStripsPort(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "192.168.88.10:54321"
	if g := ClientIP(r); g != "192.168.88.10" {
		t.Fatalf("got %q", g)
	}
	r.RemoteAddr = "[::1]:1234"
	if g := ClientIP(r); g != "::1" {
		t.Fatalf("v6 %q", g)
	}
}

func TestLoginLockoutByIP(t *testing.T) {
	s := New(&stubStore{secret: "x"})
	ip := "10.1.2.3"
	for i := 0; i < loginMaxFails; i++ {
		if s.LoginBlocked(ip) {
			t.Fatalf("blocked at %d", i)
		}
		s.RecordLogin(ip, false)
	}
	if !s.LoginBlocked(ip) {
		t.Fatal("want blocked")
	}
	s.RecordLogin("10.1.2.3:9", false)
	if s.LoginBlocked("10.1.2.3:9") {
		t.Fatal("port suffix is a different bucket; ClientIP must be used by caller")
	}
	s.RecordLogin(ip, true)
	if s.LoginBlocked(ip) {
		t.Fatal("success should clear")
	}
}

func TestSessionCSRFRoundtrip(t *testing.T) {
	s := New(&stubStore{secret: "s3cret-bytes"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	if err := s.IssueCookie(rec, req); err != nil {
		t.Fatal(err)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("no cookie")
	}
	c := cookies[0]
	if !c.HttpOnly || c.Path != cookiePath {
		t.Fatalf("cookie %+v", c)
	}
	req2 := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req2.AddCookie(c)
	if !s.ValidSession(req2) {
		t.Fatal("session")
	}
	tok := s.CSRF(req2)
	if tok == "" || !s.ValidCSRF(req2, tok) {
		t.Fatal("csrf")
	}
	if s.ValidCSRF(req2, "deadbeefdeadbeefdeadbeefdeadbeef") {
		t.Fatal("bad csrf")
	}
}

func TestSessionCookieSecureOnTLS(t *testing.T) {
	s := New(&stubStore{secret: "s3cret-bytes"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.TLS = &tls.ConnectionState{}
	if err := s.IssueCookie(rec, req); err != nil {
		t.Fatal(err)
	}
	c := rec.Result().Cookies()[0]
	if !c.Secure {
		t.Fatal("want Secure on TLS")
	}
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/admin", nil)
	if err := s.IssueCookie(rec2, req2); err != nil {
		t.Fatal(err)
	}
	if rec2.Result().Cookies()[0].Secure {
		t.Fatal("plain HTTP cookie must not be Secure")
	}
}

func TestFlashOnce(t *testing.T) {
	s := New(&stubStore{secret: "x"})
	rec := httptest.NewRecorder()
	s.PutFlash(rec, "sk-secret")
	req := httptest.NewRequest(http.MethodGet, "/admin/keys", nil)
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}
	rec2 := httptest.NewRecorder()
	if g := s.TakeFlash(rec2, req); g != "sk-secret" {
		t.Fatalf("got %q", g)
	}
	req3 := httptest.NewRequest(http.MethodGet, "/admin/keys", nil)
	for _, c := range rec.Result().Cookies() {
		req3.AddCookie(c)
	}
	if g := s.TakeFlash(httptest.NewRecorder(), req3); g != "" {
		t.Fatalf("second take %q", g)
	}
}

func TestValidMCP(t *testing.T) {
	plain := "mcp-" + "aabbccddeeff00112233445566778899aabbccdd"
	s := New(&stubStore{secret: "x", mcpHash: HashKey(plain)})
	if !s.ValidMCP(plain) {
		t.Fatal("want mcp token ok")
	}
	if !s.ValidMCP("Bearer " + plain) {
		t.Fatal("want bearer prefix stripped")
	}
	if s.ValidMCP("mcp-wrong") {
		t.Fatal("wrong token")
	}
	if s.ValidMCP("") {
		t.Fatal("empty")
	}
}
