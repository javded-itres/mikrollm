package hubclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/javded-itres/mikrollm/internal/a2aproxy"
	"github.com/javded-itres/mikrollm/internal/domain"
	"github.com/javded-itres/mikrollm/internal/mcpproxy"
	"github.com/javded-itres/mikrollm/internal/ports"
)

const maxHubBinary = 6 << 20

type Settings = domain.HubSettings

type Store interface {
	HubSettings() (Settings, error)
	SetHubSettings(Settings) error
	ListModels() ([]domain.Model, error)
	UpsertHubAuto(nodeID, nodeName, upstream string, maxContext int, media []string) error
	DeleteHubAuto() error
	DeleteHubPeers() error
	ListSharedMCP() ([]domain.MCPUpstream, error)
	MCPUpstream(name string) (domain.MCPUpstream, error)
	ListSharedA2A() ([]domain.A2AUpstream, error)
	A2AUpstream(name string) (domain.A2AUpstream, error)
}

type Client struct {
	Store       Store
	Auth        ports.Auth
	Handler     http.Handler
	Listen      string
	HubURL      string
	HTTP        *http.Client
	RelaySecret string

	wake   chan struct{}
	mu     sync.Mutex
	status string
	err    string
	runMu  sync.Mutex
	runN   map[string]int
	runCV  *sync.Cond
	catMu  sync.Mutex
	cat    Catalog
	selfID string
}

func HubURL() string {
	if v := strings.TrimSpace(os.Getenv("MIKROLLM_HUB_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return strings.TrimRight(DefaultURL, "/")
}

func New(st Store, auth ports.Auth, listen string) *Client {
	c := &Client{
		Store:  st,
		Auth:   auth,
		Listen: listen,
		HubURL: HubURL(),
		HTTP:   &http.Client{Timeout: 0, Transport: &http.Transport{ResponseHeaderTimeout: 30 * time.Second, IdleConnTimeout: 90 * time.Second}},
		wake:   make(chan struct{}, 1),
		status: "off",
		runN:   map[string]int{},
	}
	c.runCV = sync.NewCond(&c.runMu)
	return c
}

func (c *Client) URL() string {
	if c == nil || c.HubURL == "" {
		return HubURL()
	}
	return c.HubURL
}

func (c *Client) Kick() {
	if c == nil || c.wake == nil {
		return
	}
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *Client) Status() (state, err string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status, c.err
}

func (c *Client) set(state, err string) {
	c.mu.Lock()
	c.status, c.err = state, err
	c.mu.Unlock()
}

func (c *Client) Loop(ctx context.Context) {
	go c.pullLoop(ctx)
	for {
		if ctx.Err() != nil {
			return
		}
		c.RefreshCatalog()
		cfg, err := c.Store.HubSettings()
		if err != nil || !cfg.Enabled {
			c.set("off", "")
			_ = c.Store.DeleteHubAuto()
			c.sleep(ctx, 5*time.Second)
			continue
		}
		if cfg.Token == "" {
			if err := c.register(cfg); err != nil {
				c.set("error", err.Error())
				c.sleep(ctx, 8*time.Second)
				continue
			}
			cfg, _ = c.Store.HubSettings()
		}
		if err := c.announce(cfg); err != nil {
			c.set("error", err.Error())
			if isAuthErr(err) {
				cfg.Token, cfg.NodeID = "", ""
				_ = c.Store.SetHubSettings(cfg)
			}
			c.sleep(ctx, 8*time.Second)
			continue
		}
		c.set("online", "")
		c.sleep(ctx, 15*time.Second)
	}
}

func (c *Client) pullLoop(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		cfg, err := c.Store.HubSettings()
		if err != nil || !cfg.Enabled || cfg.Token == "" {
			c.sleep(ctx, 2*time.Second)
			continue
		}
		if err := c.pullOnce(ctx, cfg); err != nil {
			if ctx.Err() != nil {
				return
			}
			if isAuthErr(err) {
				cfg.Token, cfg.NodeID = "", ""
				_ = c.Store.SetHubSettings(cfg)
				c.set("error", err.Error())
			}
			c.sleep(ctx, 2*time.Second)
		}
	}
}

func (c *Client) RefreshCatalog() {
	if c.Store != nil {
		if cfg, err := c.Store.HubSettings(); err != nil || !cfg.Enabled {
			c.catMu.Lock()
			c.cat = Catalog{}
			c.selfID = ""
			c.catMu.Unlock()
			_ = c.Store.DeleteHubPeers()
			return
		}
	}
	req, err := http.NewRequest(http.MethodGet, c.URL()+"/v1/catalog", nil)
	if err != nil {
		return
	}
	cli := &http.Client{Timeout: 8 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return
	}
	var cat Catalog
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&cat) != nil {
		return
	}
	self := ""
	if cfg, err := c.Store.HubSettings(); err == nil {
		self = cfg.NodeID
	}
	c.catMu.Lock()
	c.cat = cat
	c.selfID = self
	c.catMu.Unlock()
	c.applyAuto()
}

// autoContext is the hub's value for alias auto. A positive Defaults.Context
// is either the operator override or the smallest pool context. An older hub
// omits it, so the floor is the smallest announced context in the pool.
func autoContext(def *Defaults) int {
	if def == nil {
		return 0
	}
	if def.Context > 0 {
		return def.Context
	}
	min := 0
	for _, p := range def.Pool {
		if p.Context <= 0 {
			continue
		}
		if min == 0 || p.Context < min {
			min = p.Context
		}
	}
	return min
}

func (c *Client) applyAuto() {
	if c.Store == nil {
		return
	}
	cfg, err := c.Store.HubSettings()
	if err != nil || !cfg.Enabled {
		_ = c.Store.DeleteHubAuto()
		return
	}
	c.catMu.Lock()
	def := c.cat.Defaults
	nodes := c.cat.Nodes
	c.catMu.Unlock()
	if def != nil && len(def.Pool) > 0 {
		_ = c.Store.UpsertHubAuto(domain.HubAutoRouter, "auto", domain.HubAutoAlias, autoContext(def), []string{domain.MediaChat})
		return
	}
	if def == nil || strings.TrimSpace(def.NodeID) == "" || strings.TrimSpace(def.Alias) == "" {
		_ = c.Store.DeleteHubAuto()
		return
	}
	name, ctxN, media := def.NodeName, autoContext(def), []string(nil)
	for _, n := range nodes {
		if n.ID != def.NodeID {
			continue
		}
		if n.Name != "" {
			name = n.Name
		}
		for _, a := range n.Aliases {
			if a.Alias == def.Alias {
				if ctxN <= 0 {
					ctxN = a.Context
				}
				media = a.Media
			}
		}
	}
	_ = c.Store.UpsertHubAuto(def.NodeID, name, def.Alias, ctxN, media)
}

func (c *Client) Peers() (selfID string, peers []domain.HubPeer) {
	if c.Store != nil {
		if cfg, err := c.Store.HubSettings(); err != nil || !cfg.Enabled {
			return "", nil
		}
	}
	c.catMu.Lock()
	cat := c.cat
	selfID = c.selfID
	c.catMu.Unlock()
	if selfID == "" {
		if cfg, err := c.Store.HubSettings(); err == nil {
			selfID = cfg.NodeID
		}
	}
	for _, n := range cat.Nodes {
		if n.ID == "" || n.ID == selfID {
			continue
		}
		p := domain.HubPeer{ID: n.ID, Name: n.Name, Online: n.Online, Rating: n.Rating, SharingNow: n.SharingNow}
		if n.Schedule != nil {
			p.Schedule = domain.HubSchedule{
				Enabled: n.Schedule.Enabled, Days: n.Schedule.Days,
				Start: n.Schedule.Start, End: n.Schedule.End, TZ: n.Schedule.TZ,
			}
		}
		if p.Name == "" {
			p.Name = n.ID
			if len(p.Name) > 10 {
				p.Name = p.Name[:10]
			}
		}
		for _, a := range n.Aliases {
			if a.Alias == "" {
				continue
			}
			p.Aliases = append(p.Aliases, domain.HubPeerAlias{Alias: a.Alias, Media: a.Media, Context: a.Context})
		}
		for _, m := range n.MCPs {
			if m.Name == "" {
				continue
			}
			p.MCPs = append(p.MCPs, domain.HubPeerMCP{Name: m.Name})
		}
		for _, m := range n.Agents {
			if m.Name == "" {
				continue
			}
			p.Agents = append(p.Agents, domain.HubPeerMCP{Name: m.Name})
		}
		peers = append(peers, p)
	}
	return selfID, peers
}

func (c *Client) sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	case <-c.wake:
	}
}

func (c *Client) register(cfg Settings) error {
	name := cfg.Name
	if name == "" {
		name = "mikrollm"
	}
	body, _ := json.Marshal(RegisterReq{Name: name})
	req, err := http.NewRequest(http.MethodPost, c.HubURL+"/v1/register", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("hub register %s: %s", resp.Status, raw)
	}
	var rs RegisterRes
	if err := json.Unmarshal(raw, &rs); err != nil {
		return err
	}
	if rs.Token == "" || rs.NodeID == "" {
		return fmt.Errorf("hub register: empty token")
	}
	cfg.NodeID, cfg.Token = rs.NodeID, rs.Token
	return c.Store.SetHubSettings(cfg)
}

func (c *Client) announce(cfg Settings) error {
	ms, _ := c.Store.ListModels()
	var aliases []Alias
	for _, m := range ms {
		if !m.Enabled || !m.HubShare || domain.IsHubAuto(m) || m.Alias == domain.HubAutoAlias {
			continue
		}
		media := domain.MergeMedia(m.Media, domain.InferMedia(m.Alias, nil), domain.InferMedia(m.UpstreamName, nil))
		aliases = append(aliases, Alias{
			Alias: m.Alias, Media: media, Context: m.MaxContext,
			Parameters: domain.FallbackGenerationParams(m.Alias, m.UpstreamName, media),
		})
	}
	if aliases == nil {
		aliases = []Alias{}
	}
	name := cfg.Name
	if name == "" {
		name = "mikrollm"
	}
	caps := cfg.Caps.Norm()
	ann := AnnounceReq{Name: name, Aliases: aliases, Caps: &Caps{Chat: caps.Chat, Images: caps.Images, Videos: caps.Videos}, MCPs: []MCPName{}, Agents: []MCPName{}}
	if cfg.Enabled && cfg.ShareMCP {
		ups, err := c.Store.ListSharedMCP()
		if err != nil {
			return err
		}
		for _, u := range ups {
			if !u.Enabled || !u.HubShare || u.Name == "" {
				continue
			}
			ann.MCPs = append(ann.MCPs, MCPName{Name: u.Name})
		}
	}
	if cfg.Enabled && cfg.ShareA2A {
		ups, err := c.Store.ListSharedA2A()
		if err != nil {
			return err
		}
		for _, u := range ups {
			if !u.Enabled || !u.HubShare || u.Name == "" {
				continue
			}
			ann.Agents = append(ann.Agents, MCPName{Name: u.Name})
		}
	}
	if cfg.Schedule.Enabled {
		ann.Schedule = &ShareSchedule{
			Enabled: true, Days: cfg.Schedule.Days,
			Start: cfg.Schedule.Start, End: cfg.Schedule.End, TZ: cfg.Schedule.TZ,
		}
	}
	body, _ := json.Marshal(ann)
	req, err := http.NewRequest(http.MethodPut, c.HubURL+"/v1/announce", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("announce %s: %s", resp.Status, raw)
	}
	return nil
}

// isAuthErr is only the hub's unknown-token reply. A 401 from a proxy, or a
// 503 while the hub database is busy, must keep the saved node id.
func isAuthErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), `"bad token"`)
}

func (c *Client) pullOnce(ctx context.Context, cfg Settings) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.HubURL+"/v1/pull", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	cli := &http.Client{Timeout: 25 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("pull %s: %s", resp.Status, raw)
	}
	var job Job
	if json.NewDecoder(resp.Body).Decode(&job) != nil || job.ID == "" {
		return nil
	}
	go c.runJob(cfg, job)
	return nil
}

func (c *Client) runJob(cfg Settings, job Job) {
	switch job.Kind {
	case "mcp":
		c.runMCP(cfg, job)
		return
	case "a2a":
		c.runA2A(cfg, job)
		return
	}
	if c.Handler == nil {
		c.postResult(cfg, Result{JobID: job.ID, Status: 503, Body: []byte(`{"error":"no handler"}`)})
		return
	}
	if !c.aliasShared(job.Alias) {
		c.postResult(cfg, Result{JobID: job.ID, Status: 404, Body: []byte(`{"error":{"message":"alias not shared"}}`)})
		return
	}
	if !cfg.Schedule.SharingAt(time.Now()) {
		c.postResult(cfg, Result{JobID: job.ID, Status: 503, Body: []byte(`{"error":{"message":"sharing window closed"}}`)})
		return
	}
	kind := job.Kind
	if kind == "" {
		kind = "chat"
	}
	c.acquire(kind)
	defer c.release(kind)
	method, path, payload := localRelay(job)
	var rdr io.Reader
	if payload != nil {
		rdr = bytes.NewReader(payload)
	}
	req, err := http.NewRequest(method, path, rdr)
	if err != nil {
		c.postResult(cfg, Result{JobID: job.ID, Status: 500, Body: []byte(`{"error":"bad relay"}`)})
		return
	}
	req.RemoteAddr = "127.0.0.1:1"
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.RelaySecret != "" {
		req.Header.Set(RelayHeader, c.RelaySecret)
	}
	rec := newRecorder()
	c.Handler.ServeHTTP(rec, req)
	ct := rec.h.Get("Content-Type")
	raw := rec.buf.Bytes()
	if rec.code == 0 {
		rec.code = 200
	}
	if kind == "videos_content" || (len(raw) > 0 && !json.Valid(bytes.TrimSpace(raw))) {
		if len(raw) > maxHubBinary {
			c.postResult(cfg, Result{JobID: job.ID, Status: 413, Body: []byte(`{"error":{"message":"hub media too large (6 MiB)"}}`)})
			return
		}
		if ct == "" {
			ct = "application/octet-stream"
		}
		c.postResult(cfg, Result{JobID: job.ID, Status: rec.code, ContentType: ct, B64: base64.StdEncoding.EncodeToString(raw)})
		return
	}
	if kind == "chat" {
		raw = CollapseChat(raw)
	}
	c.postResult(cfg, Result{JobID: job.ID, Status: rec.code, Body: raw})
}

func (c *Client) runMCP(cfg Settings, job Job) {
	if !cfg.Enabled || !cfg.ShareMCP {
		c.postResult(cfg, Result{JobID: job.ID, Status: 404, Body: []byte(`{"error":"mcp not shared"}`)})
		return
	}
	if !cfg.Schedule.SharingAt(time.Now()) {
		c.postResult(cfg, Result{JobID: job.ID, Status: 503, Body: []byte(`{"error":"sharing window closed"}`)})
		return
	}
	up, err := c.Store.MCPUpstream(job.Alias)
	if err != nil || !up.Enabled || !up.HubShare {
		c.postResult(cfg, Result{JobID: job.ID, Status: 404, Body: []byte(`{"error":"mcp not shared"}`)})
		return
	}
	var env MCPRelay
	_ = json.Unmarshal(job.Body, &env)
	c.acquire("mcp")
	defer c.release("mcp")
	payload := env.Payload()
	ct := env.ContentType
	if ct == "" && len(payload) > 0 && (env.Method == "" || strings.EqualFold(env.Method, http.MethodPost)) {
		ct = "application/json"
	}
	rep, err := mcpproxy.Do(context.Background(), up, mcpproxy.Forward{
		Method: env.Method, Session: env.Session, Accept: env.Accept,
		Protocol: env.Protocol, ContentType: ct, Body: payload,
	})
	if err != nil {
		c.postResult(cfg, Result{JobID: job.ID, Status: 502, Body: []byte(`{"error":"upstream mcp"}`)})
		return
	}
	res := Result{JobID: job.ID, Status: rep.Status, ContentType: rep.ContentType, Session: rep.Session, Protocol: rep.Protocol}
	if json.Valid(bytes.TrimSpace(rep.Body)) {
		res.Body = rep.Body
	} else if len(rep.Body) > 0 {
		res.B64 = base64.StdEncoding.EncodeToString(rep.Body)
	}
	c.postResult(cfg, res)
}

func (c *Client) runA2A(cfg Settings, job Job) {
	if !cfg.Enabled || !cfg.ShareA2A {
		c.postResult(cfg, Result{JobID: job.ID, Status: 404, Body: []byte(`{"error":"agent not shared"}`)})
		return
	}
	if !cfg.Schedule.SharingAt(time.Now()) {
		c.postResult(cfg, Result{JobID: job.ID, Status: 503, Body: []byte(`{"error":"sharing window closed"}`)})
		return
	}
	up, err := c.Store.A2AUpstream(job.Alias)
	if err != nil || !up.Enabled || !up.HubShare {
		c.postResult(cfg, Result{JobID: job.ID, Status: 404, Body: []byte(`{"error":"agent not shared"}`)})
		return
	}
	var env MCPRelay
	_ = json.Unmarshal(job.Body, &env)
	target := up.URL
	if a2aproxy.IsCard(env.Path) {
		rep, err := a2aproxy.FetchCard(context.Background(), up)
		if err != nil {
			c.postResult(cfg, Result{JobID: job.ID, Status: 502, Body: []byte(`{"error":"upstream agent"}`)})
			return
		}
		c.postResult(cfg, Result{JobID: job.ID, Status: rep.Status, ContentType: "application/json", Body: rep.Body})
		return
	}
	joined, ok := a2aproxy.Join(up.URL, env.Path)
	if !ok {
		c.postResult(cfg, Result{JobID: job.ID, Status: 404, Body: []byte(`{"error":"agent not shared"}`)})
		return
	}
	target = joined
	c.acquire("a2a")
	defer c.release("a2a")
	payload := env.Payload()
	ct := env.ContentType
	if ct == "" && len(payload) > 0 && (env.Method == "" || strings.EqualFold(env.Method, http.MethodPost)) {
		ct = "application/json"
	}
	rep, err := mcpproxy.Do(context.Background(), domain.MCPUpstream{URL: target, Token: up.Token}, mcpproxy.Forward{
		Method: env.Method, Accept: env.Accept, ContentType: ct, Body: payload,
	})
	if err != nil {
		c.postResult(cfg, Result{JobID: job.ID, Status: 502, Body: []byte(`{"error":"upstream agent"}`)})
		return
	}
	res := Result{JobID: job.ID, Status: rep.Status, ContentType: rep.ContentType}
	if json.Valid(bytes.TrimSpace(rep.Body)) {
		res.Body = rep.Body
	} else if len(rep.Body) > 0 {
		res.B64 = base64.StdEncoding.EncodeToString(rep.Body)
	}
	c.postResult(cfg, res)
}

func localRelay(job Job) (method, path string, body []byte) {
	method = http.MethodPost
	path = "/v1/chat/completions"
	var raw map[string]any
	if json.Unmarshal(job.Body, &raw) != nil {
		raw = map[string]any{}
	}
	raw["model"] = job.Alias
	kind := job.Kind
	if kind == "" {
		kind = "chat"
	}
	switch kind {
	case "images":
		path = "/v1/images/generations"
	case "videos":
		path = "/v1/videos"
	case "images_status":
		method = http.MethodGet
		path = "/v1/images/" + job.Ref + "?model=" + neturl.QueryEscape(job.Alias)
		return method, path, nil
	case "videos_status":
		method = http.MethodGet
		path = "/v1/videos/" + job.Ref + "?model=" + neturl.QueryEscape(job.Alias)
		return method, path, nil
	case "videos_content":
		method = http.MethodGet
		path = "/v1/videos/" + job.Ref + "/content?model=" + neturl.QueryEscape(job.Alias)
		return method, path, nil
	default:
		raw["stream"] = false
	}
	body, _ = json.Marshal(raw)
	return method, path, body
}

func (c *Client) acquire(kind string) {
	if c.runCV == nil {
		return
	}
	capn := 4
	if cfg, err := c.Store.HubSettings(); err == nil {
		capn = cfg.Caps.For(kind)
	}
	c.runMu.Lock()
	for c.runN[kind] >= capn {
		c.runCV.Wait()
	}
	c.runN[kind]++
	c.runMu.Unlock()
}

func (c *Client) release(kind string) {
	if c.runCV == nil {
		return
	}
	c.runMu.Lock()
	c.runN[kind]--
	if c.runN[kind] < 0 {
		c.runN[kind] = 0
	}
	c.runCV.Broadcast()
	c.runMu.Unlock()
}

func (c *Client) aliasShared(alias string) bool {
	ms, err := c.Store.ListModels()
	if err != nil {
		return false
	}
	for _, m := range ms {
		if m.Alias == alias && m.Enabled && m.HubShare {
			return true
		}
	}
	return false
}

type recorder struct {
	code int
	h    http.Header
	buf  bytes.Buffer
}

func newRecorder() *recorder                    { return &recorder{h: http.Header{}, code: 200} }
func (r *recorder) Header() http.Header         { return r.h }
func (r *recorder) Write(p []byte) (int, error) { return r.buf.Write(p) }
func (r *recorder) WriteHeader(c int)           { r.code = c }

func (c *Client) postResult(cfg Settings, res Result) {
	body, _ := json.Marshal(res)
	req, err := http.NewRequest(http.MethodPost, c.HubURL+"/v1/result", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
}

func LoopbackHost(listen string) string {
	_, port, err := net.SplitHostPort(strings.TrimPrefix(listen, "http://"))
	if err != nil || port == "" {
		return "127.0.0.1:4000"
	}
	return "127.0.0.1:" + port
}
