package params

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/javded-itres/mikrollm/internal/domain"
)

func parse(t *testing.T, raw string) Profile {
	t.Helper()
	p, err := ParseProfile(raw)
	if err != nil {
		t.Fatalf("profile %s: %v", raw, err)
	}
	return p
}

func decode(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("bad json: %s", body)
	}
	return m
}

func TestParseProfileValidation(t *testing.T) {
	for _, bad := range []string{
		`{"temperature":"hot"}`,
		`{"think":"over9000"}`,
		`{"think":1}`,
		`{"num_predict":1.5}`,
		`{"locked":"think"}`,
		`{"locked":["nope"]}`,
		`{"locked":["think"]}`,
		`{"bogus_key":1}`,
	} {
		if _, err := ParseProfile(bad); err == nil {
			t.Fatalf("want error for %s", bad)
		}
	}
	p, err := ParseProfile(`{"x_custom":{"a":1},"locked":["x_custom"]}`)
	if err != nil || p.Extra["x_custom"] == nil {
		t.Fatalf("x_ passthrough: %v %+v", err, p)
	}
}

func TestInjectOllamaOpenAIPath(t *testing.T) {
	p := parse(t, `{"think":"low","temperature":0.2,"num_predict":2048,"num_ctx":65536}`)
	body := Inject([]byte(`{"model":"qwen","messages":[]}`), p, domain.KindOllama, false)
	m := decode(t, body)
	if m["temperature"] != 0.2 {
		t.Fatalf("temperature: %v", m["temperature"])
	}
	if m["max_tokens"] != float64(2048) {
		t.Fatalf("num_predict→max_tokens: %v", m["max_tokens"])
	}
	if m["reasoning_effort"] != "low" {
		t.Fatalf("think→reasoning_effort: %v", m["reasoning_effort"])
	}
	if _, ok := m["num_ctx"]; ok {
		t.Fatal("num_ctx must not leak into OpenAI-compat path")
	}
	if _, ok := m["think"]; ok {
		t.Fatal("raw think must not leak")
	}
}

func TestInjectClientWins(t *testing.T) {
	p := parse(t, `{"temperature":0.2,"num_predict":2048,"think":"low"}`)
	body := Inject([]byte(`{"messages":[],"temperature":0.9,"max_tokens":50,"reasoning":{"effort":"high"}}`), p, domain.KindOllama, false)
	m := decode(t, body)
	if m["temperature"] != 0.9 || m["max_tokens"] != float64(50) {
		t.Fatalf("client fields overwritten: %v", m)
	}
	if _, ok := m["reasoning_effort"].(string); ok {
		t.Fatal("client reasoning object must win over profile think")
	}
}

func TestInjectLockedOverrides(t *testing.T) {
	p := parse(t, `{"think":false,"num_predict":128,"locked":["think","num_predict"]}`)
	body := Inject([]byte(`{"messages":[],"max_tokens":4096,"reasoning_effort":"high"}`), p, domain.KindOllama, false)
	m := decode(t, body)
	if m["max_tokens"] != float64(128) {
		t.Fatalf("locked num_predict: %v", m["max_tokens"])
	}
	if m["reasoning_effort"] != "none" {
		t.Fatalf("locked think false: %v", m["reasoning_effort"])
	}
}

func TestInjectThinkLevels(t *testing.T) {
	cases := map[string]struct {
		raw   string
		kind  string
		check func(t *testing.T, m map[string]any)
	}{
		"ollama true":  {`{"think":true}`, domain.KindOllama, func(t *testing.T, m map[string]any) { assertEq(t, m["reasoning_effort"], "high") }},
		"ollama false": {`{"think":false}`, domain.KindOllama, func(t *testing.T, m map[string]any) { assertEq(t, m["reasoning_effort"], "none") }},
		"ollama max":   {`{"think":"max"}`, domain.KindOllama, func(t *testing.T, m map[string]any) { assertEq(t, m["reasoning_effort"], "max") }},
		"openrouter medium": {`{"think":"medium"}`, domain.KindOpenRouter, func(t *testing.T, m map[string]any) {
			assertEq(t, m["reasoning"].(map[string]any)["effort"], "medium")
		}},
		"openrouter max clamped": {`{"think":"max"}`, domain.KindOpenRouter, func(t *testing.T, m map[string]any) {
			assertEq(t, m["reasoning"].(map[string]any)["effort"], "high")
		}},
		"openrouter false": {`{"think":false}`, domain.KindOpenRouter, func(t *testing.T, m map[string]any) {
			assertEq(t, m["reasoning"].(map[string]any)["enabled"], false)
		}},
		"vllm low": {`{"think":"low"}`, domain.KindVLLM, func(t *testing.T, m map[string]any) {
			assertEq(t, m["chat_template_kwargs"].(map[string]any)["reasoning_effort"], "low")
		}},
		"vllm false": {`{"think":false}`, domain.KindVLLM, func(t *testing.T, m map[string]any) {
			assertEq(t, m["chat_template_kwargs"].(map[string]any)["enable_thinking"], false)
		}},
	}
	for _, c := range cases {
		p := parse(t, c.raw)
		m := decode(t, Inject([]byte(`{"messages":[]}`), p, c.kind, false))
		c.check(t, m)
	}
}

func TestInjectVLLMRepeatPenalty(t *testing.T) {
	p := parse(t, `{"repeat_penalty":1.1,"seed":42}`)
	m := decode(t, Inject([]byte(`{"messages":[],"chat_template_kwargs":{"enable_thinking":false}}`), p, domain.KindVLLM, false))
	assertEq(t, m["repetition_penalty"], 1.1)
	assertEq(t, m["seed"], float64(42))
	// client kwargs untouched
	assertEq(t, m["chat_template_kwargs"].(map[string]any)["enable_thinking"], false)
}

func TestInjectVLLMMergesTemplateKwargs(t *testing.T) {
	p := parse(t, `{"think":"high"}`)
	m := decode(t, Inject([]byte(`{"messages":[],"chat_template_kwargs":{"enable_thinking":false}}`), p, domain.KindVLLM, false))
	tck := m["chat_template_kwargs"].(map[string]any)
	if _, ok := tck["reasoning_effort"]; ok {
		t.Fatal("client enable_thinking must win")
	}
}

func TestInjectNative(t *testing.T) {
	p := parse(t, `{"think":"low","temperature":0.1,"num_predict":512,"num_ctx":32768,"top_k":40}`)
	m := decode(t, Inject([]byte(`{"model":"qwen","messages":[],"options":{"num_ctx":8192}}`), p, domain.KindOllama, true))
	opts := m["options"].(map[string]any)
	assertEq(t, opts["temperature"], 0.1)
	assertEq(t, opts["num_predict"], float64(512))
	assertEq(t, opts["top_k"], float64(40))
	assertEq(t, opts["num_ctx"], float64(8192)) // client option kept
	assertEq(t, m["think"], "low")
	// locked num_ctx overrides client
	p = parse(t, `{"num_ctx":32768,"locked":["num_ctx"]}`)
	m = decode(t, Inject([]byte(`{"messages":[],"options":{"num_ctx":8192}}`), p, domain.KindOllama, true))
	assertEq(t, m["options"].(map[string]any)["num_ctx"], float64(32768))
}

func TestInjectMaxTokensSynonymNative(t *testing.T) {
	p := parse(t, `{"max_tokens":777}`)
	m := decode(t, Inject([]byte(`{"messages":[]}`), p, domain.KindOllama, true))
	assertEq(t, m["options"].(map[string]any)["num_predict"], float64(777))
}

func TestInjectPassthrough(t *testing.T) {
	p := parse(t, `{"temperature":0.5}`)
	if out := Inject([]byte(`not json`), p, domain.KindOllama, false); !bytes.Equal(out, []byte(`not json`)) {
		t.Fatalf("non-JSON body mutated: %s", out)
	}
	if out := Inject([]byte(`{"messages":[]}`), Profile{}, domain.KindOllama, false); !strings.Contains(string(out), `"messages"`) {
		t.Fatal("empty profile must no-op")
	}
}

func TestInjectExtra(t *testing.T) {
	p := parse(t, `{"x_served_by":"lab","locked":["x_served_by"]}`)
	m := decode(t, Inject([]byte(`{"messages":[],"x_served_by":"client"}`), p, domain.KindOllama, false))
	assertEq(t, m["x_served_by"], "lab")
}

func assertEq(t *testing.T, got, want any) {
	t.Helper()
	if got != want {
		t.Fatalf("got %v (%T), want %v (%T)", got, got, want, want)
	}
}
