package health

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/javded-itres/mikrollm/internal/domain"
)

const ollamaPricingFixture = `<section id="model-pricing">
<h2>Model pricing</h2>
<p>Prices are per million tokens</p>
<table>
<thead><tr><th>Model</th><th>Input</th><th>Cached input</th><th>Output</th></tr></thead>
<tbody>
<tr><td><a href="/library/glm-5.3">glm-5.3</a></td><td>$1.40</td><td>$0.26</td><td>$4.40</td></tr>
<tr><td><a href="/library/gemma4">gemma4</a></td><td>$0.14</td><td>$0.05</td><td>$0.40</td></tr>
<tr><td><a href="/library/gpt-oss">gpt-oss:120b</a></td><td>$0.15</td><td>$0.014</td><td>$0.60</td></tr>
<tr><td><a href="/library/gpt-oss">gpt-oss:20b</a></td><td>$0.07</td><td>$0.035</td><td>$0.30</td></tr>
<tr><td><a href="/library/deepseek-v4-flash">deepseek-v4-flash</a></td><td>$0.22</td><td>$0.007</td><td>$0.66</td></tr>
<tr><td><a href="/library/mistral-large-3">mistral-large-3</a></td><td>$0.50</td><td>-</td><td>$1.50</td></tr>
<tr><td><a href="/library/nemotron-3-super">nemotron-3-super</a></td><td>$0.015</td><td>$0.015</td><td>$0.60</td></tr>
<tr><td><a href="/library/qwen3.5">qwen3.5:397b</a></td><td>$0.60</td><td>-</td><td>$3.60</td></tr>
</tbody>
</table>
<h3>Peak pricing</h3>
<p>Peak pricing applies between 12:00 and 18:00 UTC.</p>
<table>
<thead><tr><th>Model</th><th>Input</th><th>Cached input</th><th>Output</th></tr></thead>
<tbody>
<tr><td><a href="/library/deepseek-v4-flash">deepseek-v4-flash</a></td><td>$0.44</td><td>$0.014</td><td>$1.32</td></tr>
</tbody>
</table>
</section>`

const ollamaLibraryFixture = `<div class="pricing-block">
  <span>Cost</span><span>/1M tokens</span>
  <div data-rate="base" class="pricing-rate">$1.40</div>
  <div>input</div>
  <div data-rate="base" class="pricing-rate">$0.26</div>
  <div>cached</div>
  <div data-rate="base" class="pricing-rate">$4.40</div>
  <div>output</div>
  <div data-rate="peak" class="pricing-rate">$9.99</div>
  <div>input</div>
</div>`

func TestParseOllamaPricingHTML(t *testing.T) {
	rows := parseOllamaPricingHTML(ollamaPricingFixture)
	if len(rows) != 8 {
		t.Fatalf("rows %d %+v", len(rows), rows)
	}
	idx := indexOllamaPrices(rows)
	cases := []struct {
		name string
		in   float64
		out  float64
		ok   bool
	}{
		{"glm-5.3", 1.40, 4.40, true},
		{"glm-5.3:cloud", 1.40, 4.40, true},
		{"gemma4:31b", 0.14, 0.40, true},
		{"deepseek-v4-flash:0731", 0.22, 0.66, true},
		{"mistral-large-3:675b", 0.50, 1.50, true},
		{"gpt-oss:120b", 0.15, 0.60, true},
		{"gpt-oss:20b", 0.07, 0.30, true},
		{"qwen3.5", 0.60, 3.60, true},
		{"nemotron-3-super", 0.015, 0.60, true},
		{"gpt-oss", 0, 0, false},
	}
	for _, tc := range cases {
		p, ok := idx.lookup(tc.name)
		if ok != tc.ok {
			t.Fatalf("%s ok=%v want %v", tc.name, ok, tc.ok)
		}
		if !tc.ok {
			continue
		}
		if p.Input != tc.in || p.Output != tc.out {
			t.Fatalf("%s price in=%v out=%v want %v/%v", tc.name, p.Input, p.Output, tc.in, tc.out)
		}
	}
}

func TestParseOllamaPricingLiveHTML(t *testing.T) {
	raw, err := os.ReadFile("testdata/ollama_pricing.html")
	if err != nil {
		t.Fatal(err)
	}
	rows := parseOllamaPricingHTML(string(raw))
	if len(rows) < 15 {
		t.Fatalf("rows %d %+v", len(rows), rows)
	}
	idx := indexOllamaPrices(rows)
	p, ok := idx.lookup("glm-5.3")
	if !ok || p.Input != 1.4 || p.Output != 4.4 {
		t.Fatalf("glm-5.3 %+v ok=%v", p, ok)
	}
	p, ok = idx.lookup("deepseek-v4-flash:0731")
	if !ok || p.Input != 0.22 || p.Output != 0.66 {
		t.Fatalf("must use list price not peak %+v ok=%v", p, ok)
	}
	p, ok = idx.lookup("gemma4:31b")
	if !ok || p.Input != 0.14 {
		t.Fatalf("gemma4 tag %+v ok=%v", p, ok)
	}
	if _, ok := idx.lookup("gpt-oss"); ok {
		t.Fatal("gpt-oss family is ambiguous")
	}
}

func TestParseOllamaLibraryHTML(t *testing.T) {
	in, out, ok := parseOllamaLibraryHTML(ollamaLibraryFixture)
	if !ok || in != 1.40 || out != 4.40 {
		t.Fatalf("in=%v out=%v ok=%v", in, out, ok)
	}
}

func TestProbeOllamaCloudPrices(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ol-key" {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/api/tags":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]any{
					{"name": "glm-5.3", "size": 1},
					{"name": "gemma4:31b", "size": 2},
					{"name": "gpt-oss:120b", "size": 3},
					{"name": "deepseek-v4-flash:0731", "size": 4},
				},
			})
		case "/pricing":
			_, _ = w.Write([]byte(ollamaPricingFixture))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(up.Close)
	b := domain.Backend{ID: 7, Name: "oc", BaseURL: up.URL, Kind: "ollama-cloud", Token: "ol-key", Enabled: true}
	c := New(staticBackends{b}, up.Client())
	c.CheckOnce()
	st := c.Get(7)
	if !st.Healthy {
		t.Fatalf("unhealthy: %s", st.Error)
	}
	if !st.Priced["glm-5.3"] || st.Prompt["glm-5.3"] != 1.40 || st.Completion["glm-5.3"] != 4.40 {
		t.Fatalf("glm-5.3 %+v / %+v / %+v", st.Priced, st.Prompt, st.Completion)
	}
	if st.Prompt["gemma4:31b"] != 0.14 || st.Prompt["deepseek-v4-flash:0731"] != 0.22 {
		t.Fatalf("tag match prompt=%v", st.Prompt)
	}
	if st.Prompt["gpt-oss:120b"] != 0.15 {
		t.Fatalf("gpt-oss:120b %v", st.Prompt["gpt-oss:120b"])
	}
	cat := c.Catalog([]domain.Backend{b})
	byName := map[string]domain.CatalogEntry{}
	for _, e := range cat {
		byName[e.Name] = e
	}
	if !byName["glm-5.3"].Priced || byName["glm-5.3"].PromptUSD != 1.40 {
		t.Fatalf("catalog %+v", byName["glm-5.3"])
	}
	if domain.PriceLabel(true, byName["glm-5.3"].PromptUSD, byName["glm-5.3"].CompletionUSD) != "$1.40 / $4.40" {
		t.Fatalf("label %q", domain.PriceLabel(true, byName["glm-5.3"].PromptUSD, byName["glm-5.3"].CompletionUSD))
	}
}

func TestProbeOllamaCloudLibraryFallback(t *testing.T) {
	var sawLibrary string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]any{{"name": "glm-5.3", "size": 1}},
			})
		case "/library/glm-5.3":
			sawLibrary = r.URL.Path
			_, _ = w.Write([]byte(ollamaLibraryFixture))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(up.Close)
	b := domain.Backend{ID: 8, Name: "oc", BaseURL: up.URL, Kind: "ollama-cloud", Token: "ol-key", Enabled: true}
	c := New(staticBackends{b}, up.Client())
	c.CheckOnce()
	st := c.Get(8)
	if sawLibrary != "/library/glm-5.3" {
		t.Fatalf("library %q", sawLibrary)
	}
	if !st.Priced["glm-5.3"] || st.Prompt["glm-5.3"] != 1.40 || st.Completion["glm-5.3"] != 4.40 {
		t.Fatalf("fallback %+v %+v", st.Prompt, st.Completion)
	}
}
