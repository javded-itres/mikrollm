package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/javded-itres/mikrollm/internal/admin"
	"github.com/javded-itres/mikrollm/internal/auth"
	"github.com/javded-itres/mikrollm/internal/domain"
	"github.com/javded-itres/mikrollm/internal/health"
	"github.com/javded-itres/mikrollm/internal/host"
	"github.com/javded-itres/mikrollm/internal/hubclient"
	"github.com/javded-itres/mikrollm/internal/jobs"
	"github.com/javded-itres/mikrollm/internal/mcp"
	"github.com/javded-itres/mikrollm/internal/ports"
	"github.com/javded-itres/mikrollm/internal/proxy"
	"github.com/javded-itres/mikrollm/internal/queue"
	"github.com/javded-itres/mikrollm/internal/store"
)

var (
	_ ports.Store       = (*store.Store)(nil)
	_ ports.Health      = (*health.Checker)(nil)
	_ ports.Auth        = (*auth.Service)(nil)
	_ ports.Host        = (*host.Manager)(nil)
	_ ports.Jobs        = (*jobs.Tracker)(nil)
	_ ports.HTTPGetter  = (*http.Client)(nil)
	_ ports.HTTPDoer    = (*http.Client)(nil)
	_ ports.JobRepo     = (*store.Store)(nil)
	_ ports.ChatGateway = (*proxy.Proxy)(nil)
)

type Config struct {
	Listen        string
	DataDir       string
	AdminPassword string
	ResetPassword bool
	MCPToken      string
	ResetMCPToken bool
	Version       string
	Seed          string
	SeedOllamaURL string
}

type App struct {
	Handler http.Handler
	Store   *store.Store
	stop    context.CancelFunc
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
	if _, err := st.EnsureMCPToken(cfg.MCPToken, cfg.ResetMCPToken); err != nil {
		st.Close()
		return nil, err
	}
	_ = st.SeedIfEmpty(seedBackends(cfg))

	checker := health.New(st, nil)
	if isLocalSeed(cfg.Seed) {
		bootstrapLocalOllama(st, checker)
	}
	go checker.Loop(10 * time.Second)
	keys := auth.New(st)
	hosts := host.New(nil)
	tracker := jobs.New(st)
	resumePulls(tracker, st, hosts, checker)
	px := proxy.New(st, checker, keys, nil)
	queues := queue.New(st, px, queue.LimitsFromEnv())
	px.SetQueue(queues)
	go queues.Loop(context.Background())
	hubc := hubclient.New(st, keys, cfg.Listen)
	secret := newRelaySecret()
	px.SetHubRelay(secret)
	px.SetHubDial(hubc)
	checker.SetHub(hubc)
	hubc.RelaySecret = secret
	ui := admin.New(admin.Deps{
		Store: st, Health: checker, Auth: keys, Host: hosts, Jobs: tracker, Chat: px, Queues: queues, Hub: hubc,
	})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", px.Health)
	mux.HandleFunc("GET /ready", px.Ready)
	mux.HandleFunc("POST /v1/chat/completions", px.ChatCompletions)
	mux.HandleFunc("POST /v1/images/generations", px.ImagesGenerations)
	mux.HandleFunc("POST /v1/videos", px.VideosCreate)
	mux.HandleFunc("GET /v1/videos/{id}", px.VideosGet)
	mux.HandleFunc("GET /v1/videos/{id}/content", px.VideosContent)
	mux.HandleFunc("GET /v1/models", px.ListModels)
	mux.HandleFunc("GET /v1/model/info", px.ModelInfo)
	mux.HandleFunc("GET /model/info", px.ModelInfo)
	mux.HandleFunc("POST /api/chat", px.OllamaChat)
	mux.HandleFunc("GET /api/tags", px.OllamaTags)
	ui.Mount(mux)
	mcp.New(mcp.Deps{
		Store: st, Health: checker, Auth: keys, Host: hosts, Jobs: tracker, Queues: queues,
		Version: cfg.Version,
	}).Mount(mux)

	hubc.Handler = mux
	ctx, stop := context.WithCancel(context.Background())
	go hubc.Loop(ctx)

	return &App{Handler: logRequests(secureHeaders(limitBody(mux))), Store: st, stop: stop}, nil
}

func isLocalSeed(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "local" || s == "desktop"
}

func seedBackends(cfg Config) []domain.Backend {
	if isLocalSeed(cfg.Seed) {
		u := strings.TrimSpace(cfg.SeedOllamaURL)
		if u == "" {
			u = "http://127.0.0.1:11434"
		}
		return []domain.Backend{{Name: "ollama", BaseURL: u, Weight: 1, Kind: domain.KindOllama, Enabled: true}}
	}
	return []domain.Backend{
		{Name: "mac-82", BaseURL: "http://192.168.88.82:11434", Weight: 1},
		{Name: "mac-80", BaseURL: "http://192.168.88.80:11434", Weight: 1},
	}
}

func bootstrapLocalOllama(st *store.Store, h *health.Checker) {
	ms, err := st.ListModels()
	if err != nil || len(ms) > 0 {
		return
	}
	h.CheckOnce()
	bs, err := st.ListBackends()
	if err != nil {
		return
	}
	n := 0
	for _, e := range h.Catalog(bs) {
		if e.Name == "" || len(e.BackendIDs) == 0 {
			continue
		}
		if err := st.ConnectOllamaModel(e.Name, e.BackendIDs, "least_conn", e.Context); err != nil {
			log.Printf("seed connect %s: %v", e.Name, err)
			continue
		}
		n++
	}
	if n > 0 {
		log.Printf("connected %d local Ollama model(s)", n)
	}
}

func newRelaySecret() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil && r.Method != http.MethodGet && r.Method != http.MethodHead {
			n := int64(32 << 20)
			p := r.URL.Path
			if (strings.HasPrefix(p, "/admin") && p != "/admin/chat" && p != "/admin/images" && p != "/admin/videos") || p == "/mcp" || strings.HasPrefix(p, "/mcp/") {
				n = 1 << 20
			}
			r.Body = http.MaxBytesReader(w, r.Body, n)
		}
		next.ServeHTTP(w, r)
	})
}

func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Robots-Tag", "noindex, nofollow")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if strings.HasPrefix(r.URL.Path, "/admin") {
			h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; media-src 'self' data: blob:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) Close() error {
	if a.stop != nil {
		a.stop()
	}
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
		go func(j domain.Job, b domain.Backend) {
			wr := t.Writer(j.ID)
			err := host.Pull(context.Background(), b, j.Model, wr)
			_ = wr.Close()
			if err != nil {
				t.Fail(j.ID, err.Error())
				return
			}
			t.Done(j.ID)
			h.CheckOnce()
		}(j, b)
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
