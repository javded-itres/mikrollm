package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthWired(t *testing.T) {
	a, err := New(Config{DataDir: t.TempDir(), AdminPassword: "secret", ResetPassword: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	a.Handler.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("health %d", rec.Code)
	}
}
