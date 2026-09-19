package tlsconf

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
)

const letsEncryptStagingURL = "https://acme-staging-v02.api.letsencrypt.org/directory"

type Result struct {
	Config     *tls.Config
	CertFile   string
	KeyFile    string
	ACME       bool
	HTTP01     http.Handler
	HTTP01Addr string
	Hosts      []string
}

func Setup(opt Options) (Result, error) {
	acmeHosts := ParseList(opt.ACMEHosts)
	if len(acmeHosts) > 0 {
		if strings.TrimSpace(opt.CertFile) != "" || strings.TrimSpace(opt.KeyFile) != "" || opt.Auto {
			return Result{}, fmt.Errorf("acme-hosts нельзя совмещать с tls-cert / tls-auto")
		}
		for _, h := range acmeHosts {
			if err := validACMEHost(h); err != nil {
				return Result{}, err
			}
		}
		return setupACME(opt, acmeHosts)
	}
	cert, key, err := Files(opt)
	if err != nil {
		return Result{}, err
	}
	if cert == "" {
		return Result{}, nil
	}
	cfg, err := FileTLS(cert, key)
	if err != nil {
		return Result{}, err
	}
	return Result{Config: cfg, CertFile: cert, KeyFile: key}, nil
}

func setupACME(opt Options, hosts []string) (Result, error) {
	dir := strings.TrimSpace(opt.ACMEDir)
	if dir == "" {
		dir = filepath.Join(opt.DataDir, "acme")
	} else {
		dir = resolve(opt.DataDir, dir)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Result{}, err
	}
	m := &autocert.Manager{
		Cache:      autocert.DirCache(dir),
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(hosts...),
		Email:      strings.TrimSpace(opt.ACMEEmail),
	}
	if opt.ACMEStaging {
		m.Client = &acme.Client{DirectoryURL: letsEncryptStagingURL}
	}
	cfg := m.TLSConfig()
	cfg.MinVersion = tls.VersionTLS12
	httpAddr := strings.TrimSpace(opt.ACMEHTTP)
	if httpAddr == "" {
		httpAddr = ":80"
	}
	if strings.EqualFold(httpAddr, "off") || httpAddr == "-" {
		httpAddr = ""
	}
	var h http.Handler
	if httpAddr != "" {
		https := httpsBase(hosts[0], opt.Listen)
		h = m.HTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, https+r.URL.RequestURI(), http.StatusMovedPermanently)
		}))
	}
	return Result{
		Config: cfg, ACME: true, HTTP01: h, HTTP01Addr: httpAddr, Hosts: hosts,
	}, nil
}

func httpsBase(host, listen string) string {
	u := "https://" + host
	_, port, err := net.SplitHostPort(listen)
	if err != nil {
		return u
	}
	if port != "" && port != "443" {
		u += ":" + port
	}
	return u
}

func ParseList(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range strings.Split(s, ",") {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

func validACMEHost(h string) error {
	h = strings.TrimSpace(h)
	if h == "" {
		return fmt.Errorf("пустое ACME-имя")
	}
	if net.ParseIP(h) != nil {
		return fmt.Errorf("Let's Encrypt не выдаёт сертификат на IP %s — нужно DNS-имя", h)
	}
	if strings.ContainsAny(h, "/: ") || strings.Contains(h, "..") {
		return fmt.Errorf("некорректное ACME-имя %q", h)
	}
	if h == "localhost" || !strings.Contains(h, ".") {
		return fmt.Errorf("Let's Encrypt нужен публичный FQDN, не %q", h)
	}
	return nil
}
