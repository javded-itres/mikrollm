package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/javded-itres/mikrollm/internal/app"
	"github.com/javded-itres/mikrollm/internal/tlsconf"
)

var version = "dev"

func main() {
	listen := flag.String("listen", env("MIKROLLM_LISTEN", ":4000"), "listen address")
	dataDir := flag.String("data", env("MIKROLLM_DATA", "./data"), "data directory")
	adminPass := flag.String("admin-password", os.Getenv("ADMIN_PASSWORD"), "set/reset admin password")
	resetPass := flag.Bool("admin-password-reset", os.Getenv("ADMIN_PASSWORD_RESET") == "1", "overwrite stored admin password")
	mcpToken := flag.String("mcp-token", os.Getenv("MIKROLLM_MCP_TOKEN"), "MCP bearer token (stored hashed)")
	mcpReset := flag.Bool("mcp-token-reset", os.Getenv("MIKROLLM_MCP_TOKEN_RESET") == "1", "overwrite stored MCP token")
	tlsCert := flag.String("tls-cert", os.Getenv("MIKROLLM_TLS_CERT"), "TLS certificate PEM (with tls-key)")
	tlsKey := flag.String("tls-key", os.Getenv("MIKROLLM_TLS_KEY"), "TLS private key PEM (with tls-cert)")
	tlsAuto := flag.Bool("tls-auto", os.Getenv("MIKROLLM_TLS_AUTO") == "1", "self-signed cert in <data>/tls if no tls-cert")
	tlsHosts := flag.String("tls-hosts", env("MIKROLLM_TLS_HOSTS", ""), "SANs for -tls-auto (comma-separated hosts/IPs)")
	acmeHosts := flag.String("acme-hosts", env("MIKROLLM_ACME_HOSTS", ""), "Let's Encrypt hostnames (comma-separated FQDN)")
	acmeEmail := flag.String("acme-email", env("MIKROLLM_ACME_EMAIL", ""), "contact email for Let's Encrypt")
	acmeHTTP := flag.String("acme-http", env("MIKROLLM_ACME_HTTP", ""), "HTTP-01 listen address (default :80, off to disable)")
	acmeDir := flag.String("acme-dir", env("MIKROLLM_ACME_DIR", ""), "ACME cache directory (default <data>/acme)")
	acmeStaging := flag.Bool("acme-staging", os.Getenv("MIKROLLM_ACME_STAGING") == "1", "use Let's Encrypt staging CA")
	flag.Parse()

	application, err := app.New(app.Config{
		Listen:        *listen,
		DataDir:       *dataDir,
		AdminPassword: *adminPass,
		ResetPassword: *resetPass,
		MCPToken:      *mcpToken,
		ResetMCPToken: *mcpReset,
		Version:       version,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer application.Close()

	tlsRes, err := tlsconf.Setup(tlsconf.Options{
		CertFile: *tlsCert, KeyFile: *tlsKey, Auto: *tlsAuto,
		DataDir: *dataDir, Hosts: *tlsHosts, Listen: *listen,
		ACMEHosts: *acmeHosts, ACMEEmail: *acmeEmail,
		ACMEHTTP: *acmeHTTP, ACMEDir: *acmeDir, ACMEStaging: *acmeStaging,
	})
	if err != nil {
		log.Fatal(err)
	}

	srv := &http.Server{
		Addr:              *listen,
		Handler:           application.Handler,
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 16,
		TLSConfig:         tlsRes.Config,
	}
	if tlsRes.HTTP01 != nil && tlsRes.HTTP01Addr != "" {
		go func() {
			hs := &http.Server{
				Addr:              tlsRes.HTTP01Addr,
				Handler:           tlsRes.HTTP01,
				ReadHeaderTimeout: 10 * time.Second,
				MaxHeaderBytes:    1 << 14,
			}
			log.Printf("ACME HTTP-01 on %s (Let's Encrypt ToS accepted)", tlsRes.HTTP01Addr)
			log.Fatal(hs.ListenAndServe())
		}()
	}
	if tlsRes.Config != nil {
		if tlsRes.ACME {
			kind := "Let's Encrypt"
			if *acmeStaging {
				kind += " staging"
			}
			log.Printf("mikrollm %s listening https %s (%s %s, data %s)", version, *listen, kind, tlsRes.Hosts, *dataDir)
		} else {
			log.Printf("mikrollm %s listening https %s (data %s, cert %s)", version, *listen, *dataDir, tlsRes.CertFile)
		}
		log.Fatal(srv.ListenAndServeTLS("", ""))
	}
	log.Printf("mikrollm %s listening on %s (data %s)", version, *listen, *dataDir)
	log.Fatal(srv.ListenAndServe())
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
