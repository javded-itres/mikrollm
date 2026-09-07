package app

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/javded-itres/mikrollm/internal/admin"
	"github.com/javded-itres/mikrollm/internal/auth"
	"github.com/javded-itres/mikrollm/internal/domain"
	"github.com/javded-itres/mikrollm/internal/health"
	"github.com/javded-itres/mikrollm/internal/jobs"
	"github.com/javded-itres/mikrollm/internal/ollama"
	"github.com/javded-itres/mikrollm/internal/ports"
	"github.com/javded-itres/mikrollm/internal/proxy"
	"github.com/javded-itres/mikrollm/internal/store"
)

var (
	_ ports.Store      = (*store.Store)(nil)
	_ ports.Health     = (*health.Checker)(nil)
	_ ports.Auth       = (*auth.Service)(nil)
	_ ports.Host       = (*ollama.Client)(nil)
	_ ports.Jobs       = (*jobs.Tracker)(nil)
	_ ports.HTTPGetter = (*http.Client)(nil)
	_ ports.HTTPDoer   = (*http.Client)(nil)
	_ ports.JobRepo     = (*store.Store)(nil)
	_ ports.ChatGateway = (*proxy.Proxy)(nil)
)

type Config struct {
	Listen        string
	DataDir       string
	AdminPassword string
	ResetPassword bool
}

type App struct {
	Handler http.Handler
	Store   *store.Store
}

func New(cfg Config) (*App, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return nil, err
	}
	st, err := store.Open(filepath.Join(cfg.DataDir, "mikrollm.db"))
	if err != nil {
		return nil, err
	}
	if err := st.EnsureAdmin(cfg.AdminPassword, cfg.ResetPassword); err != nil {
		st.Close()
		return nil, err
	}
	_ = st.SeedIfEmpty([]domain.Backend{
		{Name: "mac-82", BaseURL: "http://192.168.88.82:11434", Weight: 1},
		{Name: "mac-80", BaseURL: "http://192.168.88.80:11434", Weight: 1},
	})

	checker := health.New(st, nil)
	go checker.Loop(10 * time.Second)
	keys := auth.New(st)
	host := ollama.New(nil)
	tracker := jobs.New(st)
	resumePulls(tracker, st, host, checker)
	px := proxy.New(st, checker, keys, nil)
	ui := admin.New(admin.Deps{
		Store: st, Health: checker, Auth: keys, Host: host, Jobs: tracker, Chat: px,
	})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", px.Health)
	mux.HandleFunc("GET /ready", px.Ready)
	mux.HandleFunc("POST /v1/chat/completions", px.ChatCompletions)
	mux.HandleFunc("GET /v1/models", px.ListModels)
	mux.HandleFunc("POST /api/chat", px.OllamaChat)
	mux.HandleFunc("GET /api/tags", px.OllamaTags)
	ui.Mount(mux)

	return &App{Handler: logRequests(mux), Store: st}, nil
}

func (a *App) Close() error {
	if a.Store == nil {
		return nil
	}
	return a.Store.Close()
}

func resumePulls(t *jobs.Tracker, backends ports.BackendQuery, host ports.Host, h ports.Health) {
	for _, j := range t.List() {
		if j.Status != "running" {
			continue
		}
		if j.Kind != "pull" {
			t.Fail(j.ID, "прервано перезапуском шлюза")
			continue
		}
		b, err := backends.GetBackend(j.BackendID)
		if err != nil || !b.Enabled {
			t.Fail(j.ID, "сервер недоступен")
			continue
		}
		go func(j domain.Job, base string) {
			wr := t.Writer(j.ID)
			err := host.Pull(context.Background(), base, j.Model, wr)
			_ = wr.Close()
			if err != nil {
				t.Fail(j.ID, err.Error())
				return
			}
			t.Done(j.ID)
			h.CheckOnce()
		}(j, b.BaseURL)
	}
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if r.URL.Path != "/health" {
			log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Truncate(time.Millisecond))
		}
	})
}
