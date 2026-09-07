package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	http *http.Client
}

func New(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 0,
			Transport: &http.Transport{
				ResponseHeaderTimeout: 10 * time.Minute,
				IdleConnTimeout:       120 * time.Second,
			},
		}
	}
	return &Client{http: httpClient}
}

func (c *Client) Pull(ctx context.Context, base, model string, w io.Writer) error {
	body, _ := json.Marshal(map[string]any{"model": model, "name": model, "stream": true})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/api/pull", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("ollama pull %s: %s", resp.Status, bytes.TrimSpace(b))
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

func (c *Client) Delete(ctx context.Context, base, model string) error {
	body, _ := json.Marshal(map[string]any{"model": model, "name": model})
	url := strings.TrimRight(base, "/") + "/api/delete"
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		req, _ = http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err = c.http.Do(req)
		if err != nil {
			return err
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		if resp.StatusCode >= 400 {
			return fmt.Errorf("ollama delete %s: %s", resp.Status, bytes.TrimSpace(b))
		}
	}
	return nil
}

func (c *Client) Unload(ctx context.Context, base, model string) error {
	return c.keepAlive(ctx, base, model, 0, "unload")
}

// Load pins the model in RAM (keep_alive = -1).
func (c *Client) Load(ctx context.Context, base, model string) error {
	return c.keepAlive(ctx, base, model, -1, "load")
}

func (c *Client) keepAlive(ctx context.Context, base, model string, alive int, op string) error {
	body, _ := json.Marshal(map[string]any{
		"model": model, "prompt": "", "keep_alive": alive, "stream": false,
		"options": map[string]any{"num_predict": 0},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("ollama %s %s: %s", op, resp.Status, bytes.TrimSpace(b))
	}
	return nil
}
