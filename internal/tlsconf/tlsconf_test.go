package tlsconf

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseHostsDefault(t *testing.T) {
	hs := ParseHosts("  ")
	if len(hs) < 2 {
		t.Fatalf("%v", hs)
	}
	got := ParseHosts("192.168.88.1, localhost, 192.168.88.1")
	if len(got) != 2 || got[0] != "192.168.88.1" || got[1] != "localhost" {
		t.Fatalf("%v", got)
	}
}

func TestFilesHTTPWhenDisabled(t *testing.T) {
	c, k, err := Files(Options{DataDir: t.TempDir()})
	if err != nil || c != "" || k != "" {
		t.Fatalf("http %q %q %v", c, k, err)
	}
}

func TestFilesRequirePair(t *testing.T) {
	_, _, err := Files(Options{CertFile: "a.pem"})
	if err == nil {
		t.Fatal("want pair error")
	}
}

func TestAutoCreatesAndHandshakes(t *testing.T) {
	dir := t.TempDir()
	cert, key, err := Files(Options{Auto: true, DataDir: dir, Hosts: "127.0.0.1,localhost"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cert); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(key); err != nil || st.Mode().Perm()&0o077 != 0 {
		t.Fatalf("key perm %v %v", st, err)
	}
	cert2, key2, err := Files(Options{Auto: true, DataDir: dir, Hosts: "127.0.0.1"})
	if err != nil || cert2 != cert || key2 != key {
		t.Fatalf("reuse %v %s %s", err, cert2, key2)
	}

	raw, err := os.ReadFile(cert)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(raw) {
		t.Fatal("pool")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.TLS == nil {
				t.Error("no tls on request")
			}
			_, _ = io.WriteString(w, "ok")
		}),
		TLSConfig:         ServerTLS(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = srv.ServeTLS(ln, cert, key) }()
	defer srv.Close()

	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
	}}
	resp, err := client.Get("https://" + ln.Addr().String() + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || string(body) != "ok" {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
}

func TestRelativeCertPaths(t *testing.T) {
	dir := t.TempDir()
	if err := Ensure(filepath.Join(dir, "tls", autoCertName), filepath.Join(dir, "tls", autoKeyName), []string{"localhost"}); err != nil {
		t.Fatal(err)
	}
	c, k, err := Files(Options{DataDir: dir, CertFile: "tls/cert.pem", KeyFile: "tls/key.pem"})
	if err != nil {
		t.Fatal(err)
	}
	if c != filepath.Join(dir, "tls", autoCertName) || k != filepath.Join(dir, "tls", autoKeyName) {
		t.Fatalf("%s %s", c, k)
	}
}
