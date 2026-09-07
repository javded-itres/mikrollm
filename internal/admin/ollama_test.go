package admin

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/javded-itres/mikrollm/internal/web"
)

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
	fm := template.FuncMap{"hsize": humanSize, "join": strings.Join}
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
