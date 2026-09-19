package tlsconf

import (
	"crypto/tls"
	"os"
	"sync"
	"time"
)

type fileCerts struct {
	certFile string
	keyFile  string

	mu   sync.Mutex
	cert *tls.Certificate
	mtC  time.Time
	mtK  time.Time
}

func FileTLS(certFile, keyFile string) (*tls.Config, error) {
	f := &fileCerts{certFile: certFile, keyFile: keyFile}
	if err := f.reload(); err != nil {
		return nil, err
	}
	return &tls.Config{
		MinVersion:     tls.VersionTLS12,
		GetCertificate: f.GetCertificate,
	}, nil
}

func (f *fileCerts) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	if err := f.reload(); err != nil {
		f.mu.Lock()
		c := f.cert
		f.mu.Unlock()
		if c != nil {
			return c, nil
		}
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cert, nil
}

func (f *fileCerts) reload() error {
	stC, err := os.Stat(f.certFile)
	if err != nil {
		return err
	}
	stK, err := os.Stat(f.keyFile)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cert != nil && stC.ModTime().Equal(f.mtC) && stK.ModTime().Equal(f.mtK) {
		return nil
	}
	c, err := tls.LoadX509KeyPair(f.certFile, f.keyFile)
	if err != nil {
		return err
	}
	f.cert = &c
	f.mtC = stC.ModTime()
	f.mtK = stK.ModTime()
	return nil
}
