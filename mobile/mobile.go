// Package mobile is the gomobile bind API for iOS/Android.
// Only string/error in the exported surface.
package mobile

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/javded-itres/mikrollm/internal/app"
)

var (
	mu       sync.Mutex
	srv      *http.Server
	inst     *app.App
	listen   string
	password string
	lastErr  string
)

// Start boots MikroLLM on listen (default 127.0.0.1:4000).
// dataDir is the app documents directory. Empty adminPassword generates one.
// Returns the admin password in use.
func Start(dataDir, listenAddr, adminPassword string) (string, error) {
	mu.Lock()
	defer mu.Unlock()
	if srv != nil {
		return password, nil
	}
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		return "", fmt.Errorf("dataDir is required")
	}
	listenAddr = strings.TrimSpace(listenAddr)
	if listenAddr == "" {
		listenAddr = "127.0.0.1:4000"
	}
	adminPassword = strings.TrimSpace(adminPassword)
	if adminPassword == "" {
		adminPassword = randomPass()
	}
	application, err := app.New(app.Config{
		Listen:        listenAddr,
		DataDir:       dataDir,
		AdminPassword: adminPassword,
		Seed:          "mobile",
		Version:       "ios",
	})
	if err != nil {
		return "", err
	}
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		_ = application.Close()
		return "", err
	}
	s := &http.Server{
		Handler:           application.Handler,
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 16,
	}
	srv = s
	inst = application
	listen = "http://" + ln.Addr().String()
	password = adminPassword
	lastErr = ""
	go func() {
		err := s.Serve(ln)
		if err != nil && err != http.ErrServerClosed {
			mu.Lock()
			lastErr = err.Error()
			mu.Unlock()
		}
	}()
	return password, nil
}

// Stop shuts down the embedded server.
func Stop() error {
	mu.Lock()
	s := srv
	a := inst
	srv = nil
	inst = nil
	listen = ""
	mu.Unlock()
	if s == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := s.Shutdown(ctx)
	if a != nil {
		_ = a.Close()
	}
	return err
}

// URL is the base HTTP URL (empty if not started).
func URL() string {
	mu.Lock()
	defer mu.Unlock()
	return listen
}

// Password is the admin password used at Start.
func Password() string {
	mu.Lock()
	defer mu.Unlock()
	return password
}

// LastError is the ListenAndServe error, if any.
func LastError() string {
	mu.Lock()
	defer mu.Unlock()
	return lastErr
}

func randomPass() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
