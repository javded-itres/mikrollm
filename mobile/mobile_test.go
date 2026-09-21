package mobile

import (
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

func TestStartStop(t *testing.T) {
	dir := t.TempDir()
	pass, err := Start(filepath.Join(dir, "data"), "127.0.0.1:0", "ios-test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Stop() })
	if pass != "ios-test" {
		t.Fatalf("pass %q", pass)
	}
	base := URL()
	if base == "" {
		t.Fatal("empty url")
	}
	var ok bool
	for i := 0; i < 40; i++ {
		resp, err := http.Get(base + "/health")
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == 200 {
				ok = true
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ok {
		t.Fatalf("health %s last=%s", base, LastError())
	}
	resp, err := http.Get(base + "/admin/login")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("login %d", resp.StatusCode)
	}
	if err := Stop(); err != nil {
		t.Fatal(err)
	}
}
