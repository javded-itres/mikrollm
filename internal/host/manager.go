package host

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/javded-itres/mikrollm/internal/domain"
	"github.com/javded-itres/mikrollm/internal/ollama"
)

type Manager struct {
	http   *http.Client
	ollama *ollama.Client
}

func New(httpClient *http.Client) *Manager {
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 0,
			Transport: &http.Transport{
				ResponseHeaderTimeout: 10 * time.Minute,
				IdleConnTimeout:       120 * time.Second,
			},
		}
	}
	return &Manager{http: httpClient, ollama: ollama.New(httpClient)}
}

func (m *Manager) Pull(ctx context.Context, b domain.Backend, model string, w io.Writer) error {
	switch b.KindNorm() {
	case domain.KindOllama:
		return m.ollama.Pull(ctx, b.BaseURL, model, w)
	case domain.KindLMStudio:
		return m.lmsPull(ctx, b, model, w)
	default:
		return fmt.Errorf("%s", cloudOrHint(b, "скачивание"))
	}
}

func (m *Manager) Delete(ctx context.Context, b domain.Backend, model string) error {
	if b.KindNorm() != domain.KindOllama {
		return fmt.Errorf("%s не удаляет файлы моделей через API MikroLLM", b.Label())
	}
	return m.ollama.Delete(ctx, b.BaseURL, model)
}

func (m *Manager) Load(ctx context.Context, b domain.Backend, model string) error {
	switch b.KindNorm() {
	case domain.KindOllama:
		return m.ollama.Load(ctx, b.BaseURL, model)
	case domain.KindLMStudio:
		return m.lmsJSON(ctx, b, http.MethodPost, "/api/v1/models/load", map[string]any{"model": model})
	default:
		return fmt.Errorf("%s", cloudOrHint(b, "загрузка в RAM"))
	}
}

func (m *Manager) Unload(ctx context.Context, b domain.Backend, model string) error {
	switch b.KindNorm() {
	case domain.KindOllama:
		return m.ollama.Unload(ctx, b.BaseURL, model)
	case domain.KindLMStudio:
		err := m.lmsJSON(ctx, b, http.MethodPost, "/api/v1/models/unload", map[string]any{"instance_id": model})
		if err != nil {
			err = m.lmsJSON(ctx, b, http.MethodPost, "/api/v1/models/unload", map[string]any{"model": model})
		}
		return err
	default:
		return fmt.Errorf("%s", cloudOrHint(b, "выгрузка из RAM"))
	}
}

func cloudOrHint(b domain.Backend, op string) string {
	if h := b.LoadHint(); h != "" {
		return h
	}
	return b.Label() + " не поддерживает " + op + " через API"
}

func authReq(ctx context.Context, method, url, token string, body []byte) (*http.Request, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if tok := domain.SanitizeToken(token); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	req.Header.Set("User-Agent", domain.UpstreamUserAgent)
	return req, nil
}

func (m *Manager) lmsJSON(ctx context.Context, b domain.Backend, method, path string, payload map[string]any) error {
	raw, _ := json.Marshal(payload)
	req, err := authReq(ctx, method, strings.TrimRight(b.BaseURL, "/")+path, b.Token, raw)
	if err != nil {
		return err
	}
	resp, err := m.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	slurp, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("lm studio %s: %s", resp.Status, bytes.TrimSpace(slurp))
	}
	return nil
}

func (m *Manager) lmsPull(ctx context.Context, b domain.Backend, model string, w io.Writer) error {
	writeProg := func(status string, total, done int64) {
		if w == nil {
			return
		}
		line, _ := json.Marshal(map[string]any{"status": status, "total": total, "completed": done})
		_, _ = w.Write(append(line, '\n'))
	}
	raw, _ := json.Marshal(map[string]any{"model": model})
	req, err := authReq(ctx, http.MethodPost, strings.TrimRight(b.BaseURL, "/")+"/api/v1/models/download", b.Token, raw)
	if err != nil {
		return err
	}
	resp, err := m.http.Do(req)
	if err != nil {
		return err
	}
	bdy, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("lm studio download %s: %s", resp.Status, bytes.TrimSpace(bdy))
	}
	var st struct {
		JobID  string `json:"job_id"`
		Status string `json:"status"`
		Total  int64  `json:"total_size_bytes"`
		Done   int64  `json:"downloaded_bytes"`
	}
	_ = json.Unmarshal(bdy, &st)
	if st.Status == "already_downloaded" || st.Status == "completed" {
		writeProg("success", st.Total, st.Total)
		return nil
	}
	if st.JobID == "" {
		writeProg("success", 0, 0)
		return nil
	}
	writeProg(st.Status, st.Total, st.Done)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			req, err = authReq(ctx, http.MethodGet, strings.TrimRight(b.BaseURL, "/")+"/api/v1/models/download/status/"+st.JobID, b.Token, nil)
			if err != nil {
				return err
			}
			resp, err = m.http.Do(req)
			if err != nil {
				return err
			}
			bdy, _ = io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			resp.Body.Close()
			if resp.StatusCode >= 400 {
				return fmt.Errorf("lm studio status %s: %s", resp.Status, bytes.TrimSpace(bdy))
			}
			_ = json.Unmarshal(bdy, &st)
			writeProg(st.Status, st.Total, st.Done)
			switch st.Status {
			case "completed":
				writeProg("success", st.Total, st.Total)
				return nil
			case "failed":
				return fmt.Errorf("lm studio download failed")
			}
		}
	}
}
