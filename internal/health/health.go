package health

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/javded-itres/mikrollm/internal/domain"
	"github.com/javded-itres/mikrollm/internal/ports"
)

type Status = domain.HostStatus
type CatalogEntry = domain.CatalogEntry

type Checker struct {
	backends ports.BackendQuery
	doer     ports.HTTPDoer

	mu       sync.RWMutex
	stat     map[int64]Status
	conn     map[int64]int
	ctxMu    sync.Mutex
	ctxCache map[string]int
	orMu     sync.Mutex
	orCache  map[int64]catalogSnap
	ocMu     sync.Mutex
	ocPrices ollamaPriceSnap
	ocLib    map[string]ollamaLibSnap
}

type catalogSnap struct {
	at         time.Time
	names      []string
	sizes      map[string]int64
	ctx        map[string]int
	providers  map[string]string
	titles     map[string]string
	prompt     map[string]float64
	completion map[string]float64
	priced     map[string]bool
	media      map[string][]string
	imageUSD   map[string]float64
	imageTok   map[string]float64
	videoSec   map[string]float64
}

type decodedCatalog struct {
	Names      []string
	Sizes      map[string]int64
	Contexts   map[string]int
	Providers  map[string]string
	Titles     map[string]string
	Prompt     map[string]float64
	Completion map[string]float64
	Priced     map[string]bool
	Media      map[string][]string
	ImageUSD   map[string]float64
	ImageTok   map[string]float64
	VideoSec   map[string]float64
}

const openRouterCatalogTTL = 5 * time.Minute
const openRouterCatalogMax = 16 << 20

func New(backends ports.BackendQuery, doer ports.HTTPDoer) *Checker {
	if doer == nil {
		doer = &http.Client{Timeout: 8 * time.Second, CheckRedirect: domain.NoRedirect}
	}
	return &Checker{
		backends: backends,
		doer:     doer,
		stat:     map[int64]Status{},
		conn:     map[int64]int{},
		ctxCache: map[string]int{},
		orCache:  map[int64]catalogSnap{},
		ocLib:    map[string]ollamaLibSnap{},
	}
}

func (c *Checker) Loop(every time.Duration) {
	c.CheckOnce()
	t := time.NewTicker(every)
	defer t.Stop()
	for range t.C {
		c.CheckOnce()
	}
}

func (c *Checker) CheckOnce() {
	backends, err := c.backends.ListBackends()
	if err != nil {
		return
	}
	for _, b := range backends {
		st := c.probe(b)
		c.mu.Lock()
		c.stat[b.ID] = st
		c.mu.Unlock()
	}
}

func (c *Checker) RefreshAll() {
	c.orMu.Lock()
	c.orCache = map[int64]catalogSnap{}
	c.orMu.Unlock()
	c.CheckOnce()
}

func (c *Checker) RefreshBackend(id int64) {
	if id <= 0 {
		return
	}
	c.orMu.Lock()
	delete(c.orCache, id)
	c.orMu.Unlock()
	b, err := c.backends.GetBackend(id)
	if err != nil {
		return
	}
	st := c.probe(b)
	c.mu.Lock()
	c.stat[b.ID] = st
	c.mu.Unlock()
}

func (c *Checker) probe(b domain.Backend) Status {
	start := time.Now()
	st := Status{Checked: start}
	if !b.Enabled {
		st.Error = "disabled"
		return st
	}
	switch b.KindNorm() {
	case domain.KindVLLM:
		return c.probeVLLM(b, start)
	case domain.KindLMStudio:
		return c.probeLMStudio(b, start)
	case domain.KindOpenRouter:
		return c.probeOpenRouter(b, start)
	case domain.KindOllamaCloud:
		return c.probeOllamaCloud(b, start)
	default:
		return c.probeOllama(b, start)
	}
}

func (c *Checker) doGET(b domain.Backend, path string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(b.BaseURL, "/")+path, nil)
	if err != nil {
		return nil, err
	}
	applyUpstreamHeaders(req, b)
	return c.doer.Do(req)
}

func applyUpstreamHeaders(req *http.Request, b domain.Backend) {
	domain.ApplyUpstreamHeaders(req.Header, b)
}

func (c *Checker) probeOllama(b domain.Backend, start time.Time) Status {
	st := Status{Checked: start}
	resp, err := c.doGET(b, "/api/version")
	if err != nil {
		st.Error = err.Error()
		st.Latency = time.Since(start)
		return st
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	st.Latency = time.Since(start)
	if resp.StatusCode >= 400 {
		st.Error = resp.Status
		return st
	}
	st.Healthy = true
	st.Models, st.Sizes = c.fetchOllamaTags(b)
	st.Running = c.fetchOllamaPS(b)
	st.Contexts = c.ollamaContexts(b, st.Models)
	return st
}

func (c *Checker) probeVLLM(b domain.Backend, start time.Time) Status {
	st := Status{Checked: start}
	resp, err := c.doGET(b, "/health")
	if err == nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		st.Latency = time.Since(start)
		if resp.StatusCode < 400 {
			st.Healthy = true
			st.Models, st.Sizes, st.Contexts = c.fetchOpenAIModels(b)
			st.Running = append([]string{}, st.Models...)
			return st
		}
	}
	st.Models, st.Sizes, st.Contexts = c.fetchOpenAIModels(b)
	st.Latency = time.Since(start)
	if len(st.Models) > 0 {
		st.Healthy = true
		st.Running = append([]string{}, st.Models...)
		return st
	}
	if err != nil {
		st.Error = err.Error()
	} else if resp != nil {
		st.Error = resp.Status
	} else {
		st.Error = "vllm unreachable"
	}
	return st
}

func (c *Checker) probeOpenRouter(b domain.Backend, start time.Time) Status {
	st := Status{Checked: start}
	b.Token = domain.SanitizeToken(b.Token)
	if b.Token == "" {
		st.Error = "нужен API-ключ OpenRouter"
		st.Latency = time.Since(start)
		return st
	}
	resp, err := c.doGET(b, "/key")
	st.Latency = time.Since(start)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	body := io.LimitReader(resp.Body, 4096)
	if resp.StatusCode == http.StatusNotFound {
		io.Copy(io.Discard, body)
		resp.Body.Close()
		return c.probeOpenRouterModels(b, start, true)
	}
	if resp.StatusCode >= 400 {
		st.Error = openRouterStatusError(resp.StatusCode, body)
		resp.Body.Close()
		return st
	}
	io.Copy(io.Discard, body)
	resp.Body.Close()
	st.Healthy = true
	c.applyDecoded(&st, c.openRouterCatalog(b))
	return st
}

func (c *Checker) probeOpenRouterModels(b domain.Backend, start time.Time, authOK bool) Status {
	st := Status{Checked: start, Latency: time.Since(start)}
	d, err := c.fetchOpenRouterModels(b)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	st.Healthy = authOK || len(d.Names) > 0
	c.applyDecoded(&st, d)
	return st
}

func applyDecoded(st *Status, d decodedCatalog) {
	st.Models, st.Sizes, st.Contexts = d.Names, d.Sizes, d.Contexts
	st.Providers, st.Titles = d.Providers, d.Titles
	st.Prompt, st.Completion, st.Priced = d.Prompt, d.Completion, d.Priced
	st.Media = d.Media
	st.ImageUSD, st.ImageTok, st.VideoSec = d.ImageUSD, d.ImageTok, d.VideoSec
}

func (c *Checker) applyDecoded(st *Status, d decodedCatalog) { applyDecoded(st, d) }

func (c *Checker) openRouterCatalog(b domain.Backend) decodedCatalog {
	c.orMu.Lock()
	snap, ok := c.orCache[b.ID]
	fresh := ok && time.Since(snap.at) < openRouterCatalogTTL && len(snap.names) > 0
	c.orMu.Unlock()
	if fresh {
		return snap.decoded()
	}
	d, err := c.fetchOpenRouterModels(b)
	if err != nil || len(d.Names) == 0 {
		if ok && len(snap.names) > 0 {
			return snap.decoded()
		}
		return d
	}
	c.orMu.Lock()
	c.orCache[b.ID] = catalogSnap{
		at: time.Now(), names: d.Names, sizes: d.Sizes, ctx: d.Contexts,
		providers: d.Providers, titles: d.Titles, prompt: d.Prompt, completion: d.Completion, priced: d.Priced, media: d.Media,
		imageUSD: d.ImageUSD, imageTok: d.ImageTok, videoSec: d.VideoSec,
	}
	c.orMu.Unlock()
	return d
}

func (s catalogSnap) decoded() decodedCatalog {
	return decodedCatalog{
		Names: s.names, Sizes: s.sizes, Contexts: s.ctx, Providers: s.providers,
		Titles: s.titles, Prompt: s.prompt, Completion: s.completion, Priced: s.priced, Media: s.media,
		ImageUSD: s.imageUSD, ImageTok: s.imageTok, VideoSec: s.videoSec,
	}
}

func (c *Checker) fetchOpenRouterModels(b domain.Backend) (decodedCatalog, error) {
	d, err := c.fetchOpenRouterPath(b, b.ModelsPath())
	if err != nil {
		return decodedCatalog{}, err
	}
	if v, e := c.fetchOpenRouterPath(b, "/models?output_modalities=image"); e == nil {
		d = mergeDecoded(d, v)
	}
	if v, e := c.fetchOpenRouterPath(b, "/models?output_modalities=video"); e == nil {
		d = mergeDecoded(d, v)
	}
	if v, e := c.fetchOpenRouterPath(b, "/videos/models"); e == nil {
		for _, name := range v.Names {
			v.Media[name] = domain.MergeMedia(v.Media[name], []string{domain.MediaVideo})
		}
		d = mergeDecoded(d, v)
	}
	return d, nil
}

func (c *Checker) fetchOpenRouterPath(b domain.Backend, path string) (decodedCatalog, error) {
	resp, err := c.doGET(b, path)
	if err != nil {
		return decodedCatalog{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return decodedCatalog{}, fmt.Errorf("%s", openRouterStatusError(resp.StatusCode, io.LimitReader(resp.Body, 4096)))
	}
	return decodeOpenAICatalog(io.LimitReader(resp.Body, openRouterCatalogMax)), nil
}

func mergeDecoded(a, b decodedCatalog) decodedCatalog {
	if a.Names == nil {
		a = emptyDecoded()
	}
	if a.Media == nil {
		a.Media = map[string][]string{}
	}
	seen := map[string]bool{}
	for _, n := range a.Names {
		seen[n] = true
	}
	for _, n := range b.Names {
		if n == "" {
			continue
		}
		if !seen[n] {
			a.Names = append(a.Names, n)
			seen[n] = true
		}
		if b.Sizes != nil && b.Sizes[n] > a.Sizes[n] {
			if a.Sizes == nil {
				a.Sizes = map[string]int64{}
			}
			a.Sizes[n] = b.Sizes[n]
		}
		if b.Contexts != nil && b.Contexts[n] > a.Contexts[n] {
			if a.Contexts == nil {
				a.Contexts = map[string]int{}
			}
			a.Contexts[n] = b.Contexts[n]
		}
		if b.Providers != nil && b.Providers[n] != "" {
			if a.Providers == nil {
				a.Providers = map[string]string{}
			}
			if a.Providers[n] == "" {
				a.Providers[n] = b.Providers[n]
			}
		}
		if b.Titles != nil && b.Titles[n] != "" {
			if a.Titles == nil {
				a.Titles = map[string]string{}
			}
			if a.Titles[n] == "" {
				a.Titles[n] = b.Titles[n]
			}
		}
		if b.Priced != nil && b.Priced[n] {
			if a.Priced == nil {
				a.Priced = map[string]bool{}
				a.Prompt = map[string]float64{}
				a.Completion = map[string]float64{}
			}
			a.Priced[n] = true
			a.Prompt[n] = b.Prompt[n]
			a.Completion[n] = b.Completion[n]
		}
		if b.ImageUSD != nil && b.ImageUSD[n] > 0 {
			if a.ImageUSD == nil {
				a.ImageUSD = map[string]float64{}
			}
			a.ImageUSD[n] = b.ImageUSD[n]
			a.Priced[n] = true
		}
		if b.ImageTok != nil && b.ImageTok[n] > 0 {
			if a.ImageTok == nil {
				a.ImageTok = map[string]float64{}
			}
			a.ImageTok[n] = b.ImageTok[n]
			a.Priced[n] = true
		}
		if b.VideoSec != nil && b.VideoSec[n] > 0 {
			if a.VideoSec == nil {
				a.VideoSec = map[string]float64{}
			}
			a.VideoSec[n] = b.VideoSec[n]
			a.Priced[n] = true
		}
		if b.Media != nil && len(b.Media[n]) > 0 {
			if a.Media == nil {
				a.Media = map[string][]string{}
			}
			a.Media[n] = domain.MergeMedia(a.Media[n], b.Media[n])
		}
	}
	return a
}

func emptyDecoded() decodedCatalog {
	return decodedCatalog{
		Sizes: map[string]int64{}, Contexts: map[string]int{}, Providers: map[string]string{},
		Titles: map[string]string{}, Prompt: map[string]float64{}, Completion: map[string]float64{}, Priced: map[string]bool{},
		Media: map[string][]string{}, ImageUSD: map[string]float64{}, ImageTok: map[string]float64{}, VideoSec: map[string]float64{},
	}
}

func openRouterStatusError(status int, r io.Reader) string {
	msg := readAPIError(r)
	if status == http.StatusUnauthorized {
		if msg == "" {
			return "неверный API-ключ OpenRouter"
		}
		return "OpenRouter: " + msg
	}
	if status == http.StatusForbidden {
		return openRouterForbidden(msg)
	}
	if msg != "" {
		return fmt.Sprintf("OpenRouter %d: %s", status, msg)
	}
	return fmt.Sprintf("OpenRouter %s", http.StatusText(status))
}

func openRouterForbidden(msg string) string {
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "allowlist"):
		return "OpenRouter 403: IP не в allowlist ключа. " + msg
	case strings.Contains(low, "data region") || strings.Contains(low, "eu.openrouter") || strings.Contains(low, "us.openrouter"):
		return "OpenRouter 403: не тот регион API. " + msg
	case strings.Contains(low, "provisioning"):
		return "OpenRouter 403: это provisioning-ключ, нужен обычный sk-or-v1 с openrouter.ai/keys. " + msg
	case msg == "" || strings.EqualFold(msg, "forbidden") || strings.EqualFold(msg, "403 forbidden"):
		return "OpenRouter 403: IP/регион отклонён. Контейнер должен ходить в интернет через тот же VPN, что LAN, не через ISP РФ."
	default:
		return "OpenRouter 403: " + msg
	}
}

func readAPIError(r io.Reader) string {
	raw, _ := io.ReadAll(io.LimitReader(r, 4096))
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return ""
	}
	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &payload) == nil {
		if payload.Error.Message != "" {
			return strings.TrimSpace(payload.Error.Message)
		}
		if payload.Message != "" {
			return strings.TrimSpace(payload.Message)
		}
	}
	low := strings.ToLower(s)
	if strings.Contains(low, "<html") || strings.Contains(low, "cloudflare") || strings.Contains(low, "just a moment") {
		return "Cloudflare заблокировал запрос"
	}
	if len(s) > 180 {
		s = s[:177] + "…"
	}
	if strings.Contains(s, "<") {
		return ""
	}
	return s
}

func (c *Checker) probeOllamaCloud(b domain.Backend, start time.Time) Status {
	st := Status{Checked: start}
	if b.Token == "" {
		st.Error = "нужен API-ключ ollama.com"
		st.Latency = time.Since(start)
		return st
	}
	resp, err := c.doGET(b, "/api/tags")
	st.Latency = time.Since(start)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		st.Error = "неверный API-ключ ollama.com"
		return st
	}
	if resp.StatusCode >= 400 {
		n, sz, ctx := c.fetchOpenAIModels(b)
		if len(n) > 0 {
			st.Healthy = true
			st.Models, st.Sizes, st.Contexts = n, sz, ctx
			c.applyOllamaCloudPrices(b, &st)
			return st
		}
		st.Error = resp.Status
		return st
	}
	st.Healthy = true
	st.Models, st.Sizes = decodeOllamaTags(resp.Body)
	st.Contexts = c.ollamaContexts(b, st.Models)
	c.applyOllamaCloudPrices(b, &st)
	return st
}

func (c *Checker) probeLMStudio(b domain.Backend, start time.Time) Status {
	st := Status{Checked: start}
	names, sizes, loaded, ctx, err := c.fetchLMStudio(b)
	st.Latency = time.Since(start)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	st.Healthy = true
	st.Models, st.Sizes, st.Running, st.Contexts = names, sizes, loaded, ctx
	return st
}

func (c *Checker) fetchOllamaTags(b domain.Backend) ([]string, map[string]int64) {
	resp, err := c.doGET(b, "/api/tags")
	if err != nil {
		return nil, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, nil
	}
	return decodeOllamaTags(resp.Body)
}

func decodeOllamaTags(r io.Reader) ([]string, map[string]int64) {
	var payload struct {
		Models []struct {
			Name string `json:"name"`
			Size int64  `json:"size"`
		} `json:"models"`
	}
	if json.NewDecoder(io.LimitReader(r, 2<<20)).Decode(&payload) != nil {
		return nil, nil
	}
	out := make([]string, 0, len(payload.Models))
	sizes := map[string]int64{}
	for _, m := range payload.Models {
		if m.Name == "" {
			continue
		}
		out = append(out, m.Name)
		sizes[m.Name] = m.Size
	}
	return out, sizes
}

func (c *Checker) fetchOllamaPS(b domain.Backend) []string {
	resp, err := c.doGET(b, "/api/ps")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var payload struct {
		Models []struct {
			Name  string `json:"name"`
			Model string `json:"model"`
		} `json:"models"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload) != nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, m := range payload.Models {
		n := m.Name
		if n == "" {
			n = m.Model
		}
		if n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

func (c *Checker) fetchOpenAIModels(b domain.Backend) ([]string, map[string]int64, map[string]int) {
	path := "/v1/models"
	if b.KindNorm() == domain.KindOpenRouter {
		path = b.ModelsPath()
	}
	resp, err := c.doGET(b, path)
	if err != nil {
		return nil, nil, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, nil, nil
	}
	return decodeOpenAIModels(resp.Body)
}

func decodeOpenAIModels(r io.Reader) ([]string, map[string]int64, map[string]int) {
	d := decodeOpenAICatalog(r)
	return d.Names, d.Sizes, d.Contexts
}

func decodeOpenAICatalog(r io.Reader) decodedCatalog {
	var payload struct {
		Data []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			ContextLength int    `json:"context_length"`
			MaxModelLen   int    `json:"max_model_len"`
			Architecture  *struct {
				OutputModalities []string `json:"output_modalities"`
			} `json:"architecture"`
			OutputModalities []string       `json:"output_modalities"`
			Pricing          *orPricing     `json:"pricing"`
			PricingSKUs      map[string]any `json:"pricing_skus"`
		} `json:"data"`
	}
	empty := emptyDecoded()
	if json.NewDecoder(r).Decode(&payload) != nil {
		return empty
	}
	out := empty
	out.Names = make([]string, 0, len(payload.Data))
	for _, m := range payload.Data {
		if m.ID == "" {
			continue
		}
		out.Names = append(out.Names, m.ID)
		n := m.ContextLength
		if n <= 0 {
			n = m.MaxModelLen
		}
		if n > 0 {
			out.Contexts[m.ID] = n
		}
		if m.Name != "" && m.Name != m.ID {
			out.Titles[m.ID] = m.Name
		}
		out.Providers[m.ID] = domain.ProviderOf(m.ID, domain.KindOpenRouter)
		applyOpenRouterPricing(&out, m.ID, m.Pricing, m.PricingSKUs)
		mods := append([]string{}, m.OutputModalities...)
		if m.Architecture != nil {
			mods = append(mods, m.Architecture.OutputModalities...)
		}
		if media := domain.InferMedia(m.ID, mods); len(media) > 0 {
			out.Media[m.ID] = media
		}
	}
	return out
}

type orPricing struct {
	Prompt      any `json:"prompt"`
	Completion  any `json:"completion"`
	Image       any `json:"image"`
	ImageOutput any `json:"image_output"`
	ImageToken  any `json:"image_token"`
}

func applyOpenRouterPricing(out *decodedCatalog, id string, p *orPricing, skus map[string]any) {
	if p == nil && len(skus) == 0 {
		return
	}
	if out.Priced == nil {
		out.Priced = map[string]bool{}
		out.Prompt = map[string]float64{}
		out.Completion = map[string]float64{}
	}
	if p != nil {
		out.Priced[id] = true
		out.Prompt[id] = domain.PerMillion(anyFloat(p.Prompt))
		out.Completion[id] = domain.PerMillion(anyFloat(p.Completion))
		n := anyFloat(p.ImageOutput)
		if t := anyFloat(p.ImageToken); t > n {
			n = t
		}
		if t := anyFloat(p.Image); t > n {
			n = t
		}
		if n >= 0.001 {
			if out.ImageUSD == nil {
				out.ImageUSD = map[string]float64{}
			}
			out.ImageUSD[id] = n
		} else if n > 0 {
			if out.ImageTok == nil {
				out.ImageTok = map[string]float64{}
			}
			out.ImageTok[id] = domain.PerMillion(n)
		}
	}
	if s := videoSecFromSKUs(skus); s > 0 {
		if out.VideoSec == nil {
			out.VideoSec = map[string]float64{}
		}
		out.VideoSec[id] = s
		out.Priced[id] = true
	}
}

func videoSecFromSKUs(skus map[string]any) float64 {
	if len(skus) == 0 {
		return 0
	}
	prefer := []string{"duration_seconds", "per-video-second", "duration_seconds_720p", "duration_seconds_768p", "duration_seconds_480p", "cents_per_second_output"}
	for _, k := range prefer {
		if v, ok := skus[k]; ok {
			n := anyFloat(v)
			if k == "cents_per_second_output" {
				n /= 100
			}
			if n > 0 {
				return n
			}
		}
	}
	var min float64
	for k, v := range skus {
		lk := strings.ToLower(k)
		if !strings.Contains(lk, "second") && !strings.Contains(lk, "duration") {
			continue
		}
		n := anyFloat(v)
		if strings.Contains(lk, "cent") {
			n /= 100
		}
		if n > 0 && (min == 0 || n < min) {
			min = n
		}
	}
	return min
}

func anyFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case json.Number:
		n, _ := t.Float64()
		return n
	case string:
		n, _ := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return n
	default:
		return 0
	}
}

func (c *Checker) fetchLMStudio(b domain.Backend) (names []string, sizes map[string]int64, loaded []string, ctx map[string]int, err error) {
	resp, err := c.doGET(b, "/api/v1/models")
	if err != nil {
		n, sz, ctm := c.fetchOpenAIModels(b)
		if len(n) == 0 {
			return nil, nil, nil, nil, err
		}
		return n, sz, append([]string{}, n...), ctm, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		n, sz, ctm := c.fetchOpenAIModels(b)
		if len(n) == 0 {
			return nil, nil, nil, nil, fmt.Errorf("lm studio %s", resp.Status)
		}
		return n, sz, append([]string{}, n...), ctm, nil
	}
	var payload struct {
		Models []struct {
			Key              string `json:"key"`
			ID               string `json:"id"`
			SizeBytes        int64  `json:"size_bytes"`
			MaxContextLength int    `json:"max_context_length"`
			ContextLength    int    `json:"context_length"`
			LoadedInstances  []struct {
				ContextLength int `json:"context_length"`
			} `json:"loaded_instances"`
		} `json:"models"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&payload) != nil {
		n, sz, ctm := c.fetchOpenAIModels(b)
		return n, sz, append([]string{}, n...), ctm, nil
	}
	sizes = map[string]int64{}
	ctx = map[string]int{}
	if len(payload.Models) == 0 {
		n, sz, ctm := c.fetchOpenAIModels(b)
		return n, sz, append([]string{}, n...), ctm, nil
	}
	for _, m := range payload.Models {
		n := m.Key
		if n == "" {
			n = m.ID
		}
		if n == "" {
			continue
		}
		names = append(names, n)
		sizes[n] = m.SizeBytes
		win := m.MaxContextLength
		if win <= 0 {
			win = m.ContextLength
		}
		for _, inst := range m.LoadedInstances {
			if inst.ContextLength > win {
				win = inst.ContextLength
			}
		}
		if win > 0 {
			ctx[n] = win
		}
		if len(m.LoadedInstances) > 0 {
			loaded = append(loaded, n)
		}
	}
	return names, sizes, loaded, ctx, nil
}

func (c *Checker) ollamaContexts(b domain.Backend, names []string) map[string]int {
	out := map[string]int{}
	fetched := 0
	for _, name := range names {
		if n := c.cachedContext(b.ID, name); n > 0 {
			out[name] = n
			continue
		}
		if fetched >= 4 {
			continue
		}
		n := c.fetchOllamaShowContext(b, name)
		fetched++
		if n > 0 {
			c.rememberContext(b.ID, name, n)
			out[name] = n
		}
	}
	return out
}

func (c *Checker) cachedContext(backendID int64, name string) int {
	c.ctxMu.Lock()
	defer c.ctxMu.Unlock()
	return c.ctxCache[contextKey(backendID, name)]
}

func (c *Checker) rememberContext(backendID int64, name string, n int) {
	if n <= 0 {
		return
	}
	c.ctxMu.Lock()
	c.ctxCache[contextKey(backendID, name)] = n
	c.ctxMu.Unlock()
}

func contextKey(backendID int64, name string) string {
	return strconv.FormatInt(backendID, 10) + "/" + name
}

func (c *Checker) fetchOllamaShowContext(b domain.Backend, name string) int {
	raw, _ := json.Marshal(map[string]string{"name": name, "model": name})
	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(b.BaseURL, "/")+"/api/show", strings.NewReader(string(raw)))
	if err != nil {
		return 0
	}
	req.Header.Set("Content-Type", "application/json")
	applyUpstreamHeaders(req, b)
	resp, err := c.doer.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return 0
	}
	var payload struct {
		ModelInfo map[string]any `json:"model_info"`
		Details   struct {
			ContextLength int `json:"context_length"`
		} `json:"details"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&payload) != nil {
		return 0
	}
	best := payload.Details.ContextLength
	for k, v := range payload.ModelInfo {
		if !strings.HasSuffix(k, "context_length") && k != "context_length" {
			continue
		}
		n := anyInt(v)
		if n > best {
			best = n
		}
	}
	return best
}

func anyInt(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case int64:
		return int(t)
	case json.Number:
		n, _ := t.Int64()
		return int(n)
	default:
		return 0
	}
}

func (c *Checker) Get(id int64) Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.stat[id]
}

func (c *Checker) All() map[int64]Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[int64]Status, len(c.stat))
	for k, v := range c.stat {
		out[k] = v
	}
	return out
}

func (c *Checker) HealthyCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	n := 0
	for _, s := range c.stat {
		if s.Healthy {
			n++
		}
	}
	return n
}

func (c *Checker) Inc(id int64) {
	c.mu.Lock()
	c.conn[id]++
	c.mu.Unlock()
}

func (c *Checker) Dec(id int64) {
	c.mu.Lock()
	if c.conn[id] > 0 {
		c.conn[id]--
	}
	c.mu.Unlock()
}

func (c *Checker) Conns(id int64) int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.conn[id]
}

func (c *Checker) Catalog(backends []domain.Backend) []CatalogEntry {
	type acc struct {
		ids        []int64
		names      []string
		size       int64
		loaded     []string
		ctx        int
		provider   string
		title      string
		priced     bool
		prompt     float64
		completion float64
		imageUSD   float64
		imageTok   float64
		videoSec   float64
		media      []string
	}
	seen := map[string]*acc{}
	var order []string
	for _, b := range backends {
		if !b.Enabled {
			continue
		}
		st := c.Get(b.ID)
		for _, name := range st.Models {
			a := seen[name]
			if a == nil {
				a = &acc{}
				seen[name] = a
				order = append(order, name)
			}
			a.ids = append(a.ids, b.ID)
			a.names = append(a.names, b.Name)
			if st.Sizes[name] > a.size {
				a.size = st.Sizes[name]
			}
			if st.Contexts[name] > a.ctx {
				a.ctx = st.Contexts[name]
			}
			if a.provider == "" {
				if p := st.Providers[name]; p != "" {
					a.provider = p
				} else {
					a.provider = domain.ProviderOf(name, b.KindNorm())
				}
			}
			if a.title == "" && st.Titles[name] != "" {
				a.title = st.Titles[name]
			}
			if st.Priced[name] {
				a.priced = true
				a.prompt = st.Prompt[name]
				a.completion = st.Completion[name]
			}
			if st.ImageUSD[name] > 0 {
				a.imageUSD = st.ImageUSD[name]
				a.priced = true
			}
			if st.ImageTok[name] > 0 {
				a.imageTok = st.ImageTok[name]
				a.priced = true
			}
			if st.VideoSec[name] > 0 {
				a.videoSec = st.VideoSec[name]
				a.priced = true
			}
			if len(st.Media[name]) > 0 {
				a.media = domain.MergeMedia(a.media, st.Media[name])
			} else {
				a.media = domain.MergeMedia(a.media, domain.InferMedia(name, nil))
			}
		}
		for _, name := range st.Running {
			a := seen[name]
			if a == nil {
				continue
			}
			a.loaded = append(a.loaded, b.Name)
		}
	}
	sort.Strings(order)
	out := make([]CatalogEntry, 0, len(order))
	for _, name := range order {
		a := seen[name]
		out = append(out, CatalogEntry{
			Name: name, Title: a.title, Provider: a.provider,
			BackendIDs: a.ids, BackendNames: a.names, BackendCSV: joinIDs(a.ids),
			Size: a.size, LoadedOn: a.loaded, Context: a.ctx,
			Priced: a.priced, PromptUSD: a.prompt, CompletionUSD: a.completion,
			ImageUSD: a.imageUSD, ImageTokUSD: a.imageTok, VideoSecUSD: a.videoSec, Media: a.media,
		})
	}
	return out
}

func joinIDs(ids []int64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(parts, ",")
}

func (c *Checker) HasModel(id int64, name string) bool {
	st := c.Get(id)
	if !st.Healthy {
		return false
	}
	if len(st.Models) == 0 {
		return true // version ok but tags failed; allow routing
	}
	for _, m := range st.Models {
		if m == name {
			return true
		}
	}
	return false
}
