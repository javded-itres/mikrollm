package admin

import (
	"bytes"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/javded-itres/mikrollm/internal/domain"
	"github.com/javded-itres/mikrollm/internal/web"
)

func TestRejectOp(t *testing.T) {
	vllm := domain.Backend{Kind: "vllm"}
	if msg := rejectOp(vllm, "load"); msg == "" || !strings.Contains(msg, "vllm serve") {
		t.Fatalf("vllm load: %q", msg)
	}
	if msg := rejectOp(vllm, "pull"); msg == "" {
		t.Fatal("vllm pull")
	}
	ollama := domain.Backend{Kind: "ollama"}
	if msg := rejectOp(ollama, "load"); msg != "" {
		t.Fatalf("ollama load %q", msg)
	}
	lms := domain.Backend{Kind: "lmstudio"}
	if msg := rejectOp(lms, "delete"); msg == "" {
		t.Fatal("lmstudio delete should be rejected")
	}
	if msg := rejectOp(lms, "load"); msg != "" {
		t.Fatalf("lmstudio load %q", msg)
	}
	or := domain.Backend{Kind: "openrouter"}
	if msg := rejectOp(or, "pull"); msg == "" {
		t.Fatal("openrouter pull")
	}
	cloud := domain.Backend{Kind: "ollama-cloud"}
	if msg := rejectOp(cloud, "load"); msg == "" {
		t.Fatal("ollama-cloud load")
	}
}

func TestNextPath(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/admin/ollama/1/load", strings.NewReader("next=/admin"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if g := nextPath(r); g != "/admin" {
		t.Fatalf("got %q", g)
	}
	r = httptest.NewRequest(http.MethodPost, "/admin/ollama/1/load", strings.NewReader("next=http://evil"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if g := nextPath(r); g != "/admin/models" {
		t.Fatalf("got %q", g)
	}
	if g := safeAdminPath("//evil.com", "/admin"); g != "/admin" {
		t.Fatalf("protocol-relative %q", g)
	}
	if g := safeAdminPath("/admin@evil", "/admin"); g != "/admin" {
		t.Fatalf("at-sign %q", g)
	}
	if g := safeAdminPath("/admin/models", "/admin"); g != "/admin/models" {
		t.Fatalf("ok %q", g)
	}
}

func TestTemplatesParse(t *testing.T) {
	fm := template.FuncMap{"hsize": humanSize, "hctx": domain.FormatContext, "join": strings.Join, "add": func(a, b int) int { return a + b }}
	for _, files := range [][]string{
		{"templates/layout.html", "templates/dash.html"},
		{"templates/layout.html", "templates/models.html"},
		{"templates/layout.html", "templates/chat.html"},
		{"templates/layout.html", "templates/keys.html"},
		{"templates/layout.html", "templates/queues.html"},
		{"templates/layout.html", "templates/logs.html"},
	} {
		if _, err := template.New("layout.html").Funcs(fm).ParseFS(web.FS, files...); err != nil {
			t.Fatalf("%v: %v", files, err)
		}
	}
}

func TestQueueStepDeleteUsesQueueID(t *testing.T) {
	fm := template.FuncMap{"hsize": humanSize, "hctx": domain.FormatContext, "join": strings.Join, "add": func(a, b int) int { return a + b }}
	tpl := template.Must(template.New("layout.html").Funcs(fm).ParseFS(web.FS, "templates/layout.html", "templates/queues.html"))
	var buf bytes.Buffer
	err := tpl.ExecuteTemplate(&buf, "layout.html", map[string]any{
		"Title": "Очереди", "Nav": "queues", "CSRF": "tok",
		"Queues": []domain.QueueView{{
			ID: 42, Name: "home", Alias: "chat", ExtraAliases: []string{"chat2"},
			Steps: []domain.QueueStepView{{Alias: "local", Cap: 2}, {Alias: "cloud", Cap: 1}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	for _, want := range []string{
		`/admin/queues/42/steps/0/delete`,
		`/admin/queues/42/steps/1/delete`,
		`/admin/queues/42/steps/1/up`,
		`/admin/queues/42/aliases/delete`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %s", want)
		}
	}
	if strings.Contains(html, "/admin/queues//steps/") {
		t.Fatal("empty queue id in step URL")
	}
}

func TestLogHelpers(t *testing.T) {
	if logStatusKind(200) != "2" || logStatusKind(404) != "4" || logStatusKind(502) != "5" {
		t.Fatal("kind")
	}
	got := uniqueSorted([]string{"b", "a", "", "a", " c "})
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("%v", got)
	}
}
