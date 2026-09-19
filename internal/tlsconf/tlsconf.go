package tlsconf

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	autoCertName = "cert.pem"
	autoKeyName  = "key.pem"
)

type Options struct {
	CertFile    string
	KeyFile     string
	Auto        bool
	DataDir     string
	Hosts       string
	ACMEHosts   string
	ACMEEmail   string
	ACMEHTTP    string
	ACMEDir     string
	ACMEStaging bool
	Listen      string
}

// Files returns cert and key paths. Empty strings mean plain HTTP.
func Files(opt Options) (certFile, keyFile string, err error) {
	certFile = strings.TrimSpace(opt.CertFile)
	keyFile = strings.TrimSpace(opt.KeyFile)
	if (certFile == "") != (keyFile == "") {
		return "", "", fmt.Errorf("tls-cert и tls-key нужно задать вместе")
	}
	if certFile != "" {
		certFile = resolve(opt.DataDir, certFile)
		keyFile = resolve(opt.DataDir, keyFile)
		if _, err := tls.LoadX509KeyPair(certFile, keyFile); err != nil {
			return "", "", fmt.Errorf("tls: %w", err)
		}
		return certFile, keyFile, nil
	}
	if !opt.Auto {
		return "", "", nil
	}
	dir := filepath.Join(opt.DataDir, "tls")
	certFile = filepath.Join(dir, autoCertName)
	keyFile = filepath.Join(dir, autoKeyName)
	if err := Ensure(certFile, keyFile, ParseHosts(opt.Hosts)); err != nil {
		return "", "", err
	}
	return certFile, keyFile, nil
}

func ServerTLS() *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS12}
}

func ParseHosts(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	if len(out) == 0 {
		return []string{"192.168.88.1", "192.168.254.5", "localhost", "127.0.0.1"}
	}
	return out
}

func Ensure(certFile, keyFile string, hosts []string) error {
	_, errC := os.Stat(certFile)
	_, errK := os.Stat(keyFile)
	existC := errC == nil
	existK := errK == nil
	if existC != existK {
		return fmt.Errorf("tls: нужен и сертификат, и ключ (%s, %s)", certFile, keyFile)
	}
	if existC && existK {
		_, err := tls.LoadX509KeyPair(certFile, keyFile)
		return err
	}
	certPEM, keyPEM, err := Generate(hosts)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(certFile), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(certFile, certPEM, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		return err
	}
	return nil
}

func Generate(hosts []string) (certPEM, keyPEM []byte, err error) {
	if len(hosts) == 0 {
		hosts = ParseHosts("")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}
	tpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"MikroLLM"},
			CommonName:   hosts[0],
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(825 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			tpl.IPAddresses = append(tpl.IPAddresses, ip)
		} else {
			tpl.DNSNames = append(tpl.DNSNames, h)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	raw, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: raw})
	return certPEM, keyPEM, nil
}

func resolve(dataDir, p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(dataDir, p)
}
