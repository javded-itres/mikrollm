package health

import (
	"encoding/json"
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
	client   ports.HTTPGetter

	mu   sync.RWMutex
	stat map[int64]Status
	conn map[int64]int
}

func New(backends ports.BackendQuery, client ports.HTTPGetter) *Checker {
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}
	return &Checker{
		backends: backends,
		client:   client,
		stat:     map[int64]Status{},
		conn:     map[int64]int{},
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

func (c *Checker) probe(b domain.Backend) Status {
	start := time.Now()
	st := Status{Checked: start}
	if !b.Enabled {
		st.Error = "disabled"
		return st
	}
	resp, err := c.client.Get(b.BaseURL + "/api/version")
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
	st.Models, st.Sizes = c.fetchTags(b.BaseURL)
	st.Running = c.fetchPS(b.BaseURL)
	return st
}

func (c *Checker) fetchTags(base string) ([]string, map[string]int64) {
	resp, err := c.client.Get(base + "/api/tags")
	if err != nil {
		return nil, nil
	}
	defer resp.Body.Close()
	var payload struct {
		Models []struct {
			Name string `json:"name"`
			Size int64  `json:"size"`
		} `json:"models"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&payload) != nil {
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

func (c *Checker) fetchPS(base string) []string {
	resp, err := c.client.Get(base + "/api/ps")
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
		ids    []int64
		names  []string
		size   int64
		loaded []string
	}
	seen := map[string]*acc{}
	var order []string
	for _, b := range backends {
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
			Name: name, BackendIDs: a.ids, BackendNames: a.names,
			BackendCSV: joinIDs(a.ids), Size: a.size, LoadedOn: a.loaded,
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
