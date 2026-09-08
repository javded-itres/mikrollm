package app

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

const testMCPToken = "mcp-test-token-aaaaaaaaaaaaaaaaaaaaaaaa"

func testApp(t *testing.T) *App {
	t.Helper()
	a, err := New(Config{DataDir: t.TempDir(), AdminPassword: "secret99", ResetPassword: true, MCPToken: testMCPToken, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

func TestHealthWired(t *testing.T) {
	a := testApp(t)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	a.Handler.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("health %d", rec.Code)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("headers %+v", rec.Header())
	}
}

func login(t *testing.T, a *App) []*http.Cookie {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader("password=secret99"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "192.168.88.10:1"
	rec := httptest.NewRecorder()
	a.Handler.ServeHTTP(rec, req)
	if rec.Code != 302 {
		t.Fatalf("login %d %s", rec.Code, rec.Body.String())
	}
	return rec.Result().Cookies()
}

func withCookies(req *http.Request, cookies []*http.Cookie) {
	for _, c := range cookies {
		req.AddCookie(c)
	}
}

func TestAdminCSRF(t *testing.T) {
	a := testApp(t)
	cookies := login(t, a)
	req := httptest.NewRequest(http.MethodPost, "/admin/refresh", nil)
	withCookies(req, cookies)
	rec := httptest.NewRecorder()
	a.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("want csrf 403 got %d", rec.Code)
	}

	page := httptest.NewRequest(http.MethodGet, "/admin", nil)
	withCookies(page, cookies)
	prec := httptest.NewRecorder()
	a.Handler.ServeHTTP(prec, page)
	m := regexp.MustCompile(`name="csrf-token" content="([^"]+)"`).FindStringSubmatch(prec.Body.String())
	if len(m) != 2 {
		t.Fatalf("no csrf in page")
	}
	req = httptest.NewRequest(http.MethodPost, "/admin/refresh", strings.NewReader("csrf="+m[1]+"&next=/admin"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	withCookies(req, cookies)
	rec = httptest.NewRecorder()
	a.Handler.ServeHTTP(rec, req)
	if rec.Code != 302 {
		t.Fatalf("refresh with csrf %d", rec.Code)
	}
}

func TestRefreshOpenRedirect(t *testing.T) {
	a := testApp(t)
	cookies := login(t, a)
	page := httptest.NewRequest(http.MethodGet, "/admin", nil)
	withCookies(page, cookies)
	prec := httptest.NewRecorder()
	a.Handler.ServeHTTP(prec, page)
	m := regexp.MustCompile(`name="csrf-token" content="([^"]+)"`).FindStringSubmatch(prec.Body.String())
	if len(m) != 2 {
		t.Fatal("csrf")
	}
	body := url.Values{"csrf": {m[1]}, "next": {"https://evil.example/"}}.Encode()
	req := httptest.NewRequest(http.MethodPost, "/admin/refresh", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	withCookies(req, cookies)
	rec := httptest.NewRecorder()
	a.Handler.ServeHTTP(rec, req)
	loc := rec.Header().Get("Location")
	if rec.Code != 302 || strings.Contains(loc, "evil") {
		t.Fatalf("code %d loc %q", rec.Code, loc)
	}
}

func TestStaticNoTemplates(t *testing.T) {
	a := testApp(t)
	req := httptest.NewRequest(http.MethodGet, "/admin/static/../templates/login.html", nil)
	rec := httptest.NewRecorder()
	a.Handler.ServeHTTP(rec, req)
	b, _ := io.ReadAll(rec.Result().Body)
	if rec.Code == 200 && strings.Contains(string(b), "Админка") {
		t.Fatal("served template via static")
	}
}

func TestLoginLockoutUsesIP(t *testing.T) {
	a := testApp(t)
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader("password=wrong"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = fmt.Sprintf("10.9.8.7:%d", 1000+i)
		rec := httptest.NewRecorder()
		a.Handler.ServeHTTP(rec, req)
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader("password=secret99"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "10.9.8.7:59999"
	rec := httptest.NewRecorder()
	a.Handler.ServeHTTP(rec, req)
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "too+many") && rec.Code != 302 {
		t.Fatalf("want lockout, code %d loc %q", rec.Code, rec.Header().Get("Location"))
	}
	if !strings.Contains(rec.Header().Get("Location"), "too+many") {
		t.Fatalf("loc %q", rec.Header().Get("Location"))
	}
}

func TestMCPWired(t *testing.T) {
	a := testApp(t)
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.RemoteAddr = "192.168.88.20:1"
	rec := httptest.NewRecorder()
	a.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testMCPToken)
	req.RemoteAddr = "192.168.88.20:1"
	rec = httptest.NewRecorder()
	a.Handler.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("tools/list %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "get_status") {
		t.Fatalf("body %s", rec.Body.String())
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("secure headers")
	}
}
