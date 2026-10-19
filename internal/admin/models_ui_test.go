package admin

import (
	"bytes"
	"encoding/json"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/javded-itres/mikrollm/internal/domain"
	"github.com/javded-itres/mikrollm/internal/store"
	"github.com/javded-itres/mikrollm/internal/web"
)

func TestCleanAlias(t *testing.T) {
	got, err := cleanAlias("  glm-5.1:cloud ")
	if err != nil || got != "glm-5.1:cloud" {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := cleanAlias("bad name"); err == nil {
		t.Fatal("space should be rejected")
	}
	if _, err := cleanAlias(""); err == nil {
		t.Fatal("empty")
	}
}

func TestProfileFromForm(t *testing.T) {
	form := url.Values{
		"think":       {"low"},
		"temperature": {"0.2"},
		"lock":        {"think"},
	}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	raw, err := profileFromForm(req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"think":"low"`) || !strings.Contains(raw, `"locked"`) {
		t.Fatalf("%s", raw)
	}
	bad := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("temperature=9"))
	bad.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if _, err := profileFromForm(bad); err == nil {
		t.Fatal("expected temperature error")
	}
}

type catHealth struct {
	entries []domain.CatalogEntry
}

func (catHealth) CheckOnce()                  {}
func (catHealth) RefreshAll()                 {}
func (catHealth) RefreshBackend(int64)        {}
func (catHealth) Get(int64) domain.HostStatus { return domain.HostStatus{} }
func (h catHealth) Catalog([]domain.Backend) []domain.CatalogEntry {
	return h.entries
}
func (catHealth) HealthyCount() int           { return 0 }
func (catHealth) Inc(int64)                   {}
func (catHealth) Dec(int64)                   {}
func (catHealth) Conns(int64) int             { return 0 }
func (catHealth) HasModel(int64, string) bool { return false }

func TestUpdateModelSettings(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	bid, err := st.UpsertBackend("mac", "http://127.0.0.1:11434", true, 1, "ollama", "")
	if err != nil {
		t.Fatal(err)
	}
	id, err := st.SaveModel(domain.Model{
		Alias: "qwen", UpstreamName: "qwen", LBPolicy: "least_conn", Enabled: true,
		BackendIDs: []int64{bid},
	})
	if err != nil {
		t.Fatal(err)
	}
	u := &UI{st: st, health: catHealth{entries: []domain.CatalogEntry{{
		Name: "qwen2.5", Provider: "Ollama", BackendIDs: []int64{bid}, Media: []string{"chat"},
	}}}}
	form := url.Values{
		"alias":         {"my-qwen"},
		"upstream_name": {"qwen2.5"},
		"backend_id":    {strconv.FormatInt(bid, 10)},
		"max_context":   {"8192"},
		"prompt_cache":  {"off"},
		"lb_policy":     {"failover"},
		"hub_share":     {"1"},
		"think":         {"low"},
		"temperature":   {"0.2"},
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/models/"+strconv.FormatInt(id, 10)+"/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("id", strconv.FormatInt(id, 10))
	rr := httptest.NewRecorder()
	u.updateModelSettings(rr, req)
	if rr.Code != http.StatusFound {
		t.Fatalf("status %d %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Header().Get("Location"), "ok=settings_saved") {
		t.Fatal(rr.Header().Get("Location"))
	}
	m, err := st.GetModel(id)
	if err != nil {
		t.Fatal(err)
	}
	if m.Alias != "my-qwen" || m.UpstreamName != "qwen2.5" || m.LBPolicy != "failover" || !m.HubShare || m.MaxContext != 8192 || m.PromptCache != "off" {
		t.Fatalf("%+v", m)
	}
	if len(m.BackendIDs) != 1 || m.BackendIDs[0] != bid {
		t.Fatalf("backends %+v", m.BackendIDs)
	}
	if !strings.Contains(m.Params, "low") {
		t.Fatal(m.Params)
	}

	req = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(url.Values{
		"alias": {"my qwen"}, "upstream_name": {"qwen2.5"}, "backend_id": {strconv.FormatInt(bid, 10)},
		"prompt_cache": {"off"},
	}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("id", strconv.FormatInt(id, 10))
	rr = httptest.NewRecorder()
	u.updateModelSettings(rr, req)
	if !strings.Contains(rr.Header().Get("Location"), "err=alias") {
		t.Fatal(rr.Header().Get("Location"))
	}

	hid, err := st.SaveModel(domain.Model{
		Alias: "peer", UpstreamName: "peer", LBPolicy: "least_conn", Enabled: true,
		HubNodeID: "n1", HubNodeName: "other",
	})
	if err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(url.Values{
		"alias": {"peer-local"}, "max_context": {"1024"}, "prompt_cache": {"inherit"},
	}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("id", strconv.FormatInt(hid, 10))
	rr = httptest.NewRecorder()
	u.updateModelSettings(rr, req)
	if !strings.Contains(rr.Header().Get("Location"), "ok=settings_saved") {
		t.Fatal(rr.Header().Get("Location"))
	}
	hm, err := st.GetModel(hid)
	if err != nil {
		t.Fatal(err)
	}
	if hm.Alias != "peer-local" || hm.HubNodeID != "n1" || hm.MaxContext != 1024 {
		t.Fatalf("%+v", hm)
	}
}

func TestModelsPageModals(t *testing.T) {
	fm := template.FuncMap{
		"hsize": humanSize,
		"hctx":  domain.FormatContext,
		"join":  strings.Join,
		"add":   func(a, b int) int { return a + b },
		"tr":    tr,
		"json": func(v any) string {
			b, err := json.Marshal(v)
			if err != nil {
				return "null"
			}
			return string(b)
		},
	}
	tpl := template.Must(template.New("layout.html").Funcs(fm).ParseFS(web.FS, "templates/layout.html", "templates/models.html"))
	var buf bytes.Buffer
	err := tpl.ExecuteTemplate(&buf, "layout.html", map[string]any{
		"Title": "Модели", "Nav": "models", "CSRF": "tok", "Lang": "ru",
		"Available": 1,
		"Providers": []string{"Ollama"},
		"Backends":  []domain.Backend{{ID: 3, Name: "mac", Kind: "ollama", Enabled: true}},
		"Models": []modelVM{{
			Model: domain.Model{
				ID: 7, Alias: "chat", UpstreamName: "qwen", LBPolicy: "least_conn",
				MaxContext: 4096, PromptCache: "inherit",
			},
			Provider: "Ollama", BackendNames: "mac", BackendIDsCSV: "3", CtxLabel: "4k",
		}},
		"Catalog": []catalogVM{{
			CatalogEntry: domain.CatalogEntry{Name: "qwen", Provider: "Ollama", BackendCSV: "3", Context: 4096},
			SizeLabel:    "1 GB", CtxLabel: "4k",
			Hosts: []catalogHost{{ID: 3, Name: "mac", Short: "mac", CanLoad: true, Label: "Ollama"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	for _, s := range []string{
		"js-alias-settings", `id="alias-dialog"`, `id="model-dialog"`, "js-model-settings",
		"app.css?v=42", "app.js?v=42",
	} {
		if !strings.Contains(html, s) {
			t.Fatalf("missing %s", s)
		}
	}
	if strings.Contains(html, "/admin/models/7/context") || strings.Contains(html, "/admin/models/7/fallback") || strings.Contains(html, "<th>Параметры</th>") {
		t.Fatal("alias row still has inline settings")
	}
	if !strings.Contains(html, "canLoad") {
		t.Fatal("model host payload missing")
	}
}
