package admin

import (
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
}

func TestTemplatesParse(t *testing.T) {
	fm := template.FuncMap{"hsize": humanSize, "hctx": domain.FormatContext, "join": strings.Join}
	for _, files := range [][]string{
		{"templates/layout.html", "templates/dash.html"},
		{"templates/layout.html", "templates/models.html"},
		{"templates/layout.html", "templates/chat.html"},
		{"templates/layout.html", "templates/keys.html"},
	} {
		if _, err := template.New("layout.html").Funcs(fm).ParseFS(web.FS, files...); err != nil {
			t.Fatalf("%v: %v", files, err)
		}
	}
}
