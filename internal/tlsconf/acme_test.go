package tlsconf

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidACMEHost(t *testing.T) {
	if err := validACMEHost("llm.example.com"); err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"192.168.88.1", "localhost", "llm", "https://x.com", ""} {
		if err := validACMEHost(h); err == nil {
			t.Fatalf("want error for %q", h)
		}
	}
}

func TestSetupRejectsACMEWithFiles(t *testing.T) {
	_, err := Setup(Options{ACMEHosts: "llm.example.com", Auto: true, DataDir: t.TempDir()})
	if err == nil {
		t.Fatal("want mix error")
	}
}

func TestSetupACMEHTTP01(t *testing.T) {
	dir := t.TempDir()
	r, err := Setup(Options{
		DataDir: dir, ACMEHosts: "LLM.Example.COM, llm.example.com",
		ACMEEmail: "ops@example.com", Listen: ":4000",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !r.ACME || r.Config == nil || r.HTTP01 == nil || r.HTTP01Addr != ":80" {
		t.Fatalf("%+v", r)
	}
	if len(r.Hosts) != 1 || r.Hosts[0] != "llm.example.com" {
		t.Fatalf("hosts %v", r.Hosts)
	}
	st, err := os.Stat(filepath.Join(dir, "acme"))
	if err != nil || !st.IsDir() {
		t.Fatalf("cache %v", err)
	}
}

func TestSetupACMEHTTPOff(t *testing.T) {
	r, err := Setup(Options{DataDir: t.TempDir(), ACMEHosts: "llm.example.com", ACMEHTTP: "off"})
	if err != nil {
		t.Fatal(err)
	}
	if r.HTTP01 != nil || r.HTTP01Addr != "" {
		t.Fatalf("%+v", r)
	}
}

func TestHTTPSBase(t *testing.T) {
	if g := httpsBase("llm.example.com", ":4000"); g != "https://llm.example.com:4000" {
		t.Fatal(g)
	}
	if g := httpsBase("llm.example.com", ":443"); g != "https://llm.example.com" {
		t.Fatal(g)
	}
}

func TestFileTLSReload(t *testing.T) {
	dir := t.TempDir()
	cert := filepath.Join(dir, "c.pem")
	key := filepath.Join(dir, "k.pem")
	c1, k1, err := Generate([]string{"one.example"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cert, c1, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(key, k1, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := FileTLS(cert, key)
	if err != nil {
		t.Fatal(err)
	}
	first, err := cfg.GetCertificate(nil)
	if err != nil || first == nil {
		t.Fatal(err)
	}
	c2, k2, err := Generate([]string{"two.example"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cert, c2, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(key, k2, 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := cfg.GetCertificate(nil)
	if err != nil || second == nil {
		t.Fatal(err)
	}
	if first.Certificate[0][0] == second.Certificate[0][0] && len(first.Certificate[0]) == len(second.Certificate[0]) {
		// first byte might collide; compare lengths of raw
		if string(first.Certificate[0]) == string(second.Certificate[0]) {
			t.Fatal("cert not reloaded")
		}
	}
}
