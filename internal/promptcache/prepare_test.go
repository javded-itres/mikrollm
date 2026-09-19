package promptcache

import (
	"encoding/json"
	"testing"

	"github.com/javded-itres/mikrollm/internal/domain"
)

func TestResolve(t *testing.T) {
	if Resolve("", "") != ModeAuto {
		t.Fatal(Resolve("", ""))
	}
	if Resolve(ModeOff, ModeInherit) != ModeOff {
		t.Fatal("inherit")
	}
	if Resolve(ModeOff, "garbage") != ModeOff {
		t.Fatal("garbage")
	}
	if Resolve(ModeOff, ModeOn) != ModeOn {
		t.Fatal("alias wins")
	}
}

func TestNeedsAnthropic(t *testing.T) {
	if !NeedsAnthropicTopLevel("Anthropic", "", "") {
		t.Fatal("provider")
	}
	if !NeedsAnthropicTopLevel("", "anthropic/claude-sonnet-4", "") {
		t.Fatal("sendAs")
	}
	if !NeedsAnthropicTopLevel("", "", "anthropic/claude-3") {
		t.Fatal("orig")
	}
	if NeedsAnthropicTopLevel("OpenAI", "openai/gpt-4o-mini", "fast") {
		t.Fatal("openai")
	}
	if NeedsAnthropicTopLevel("OpenRouter", "claude-sonnet-4", "claude") {
		t.Fatal("cold catalog alias")
	}
}

func TestPrepareAutoInjectsAnthropicOnly(t *testing.T) {
	body := []byte(`{"model":"claude","messages":[{"role":"user","content":"hi"}]}`)
	out, ch := Prepare(PrepInput{
		Body: body, SendAs: "anthropic/claude-sonnet-4", OrigModel: "claude",
		Kind: domain.KindOpenRouter, Provider: "Anthropic", Mode: ModeAuto,
	})
	if !ch {
		t.Fatal("want change")
	}
	var raw map[string]any
	_ = json.Unmarshal(out, &raw)
	cc, _ := raw["cache_control"].(map[string]any)
	if cc["type"] != "ephemeral" {
		t.Fatalf("%+v", raw["cache_control"])
	}
	if _, ok := cc["ttl"]; ok {
		t.Fatal("no ttl")
	}
	if raw["model"] != "anthropic/claude-sonnet-4" {
		t.Fatalf("model %v", raw["model"])
	}

	openaiBody := []byte(`{"model":"openai/gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`)
	out2, _ := Prepare(PrepInput{
		Body: openaiBody, SendAs: "openai/gpt-4o-mini", OrigModel: "fast",
		Kind: domain.KindOpenRouter, Provider: "OpenAI", Mode: ModeAuto,
	})
	var raw2 map[string]any
	_ = json.Unmarshal(out2, &raw2)
	if _, ok := raw2["cache_control"]; ok {
		t.Fatalf("openai auto inject %s", out2)
	}
}

func TestPrepareOnInjectsAnyOpenRouter(t *testing.T) {
	body := []byte(`{"model":"fast","messages":[]}`)
	out, ch := Prepare(PrepInput{
		Body: body, SendAs: "openai/gpt-4o-mini", Kind: domain.KindOpenRouter, Mode: ModeOn,
	})
	if !ch {
		t.Fatal("want inject")
	}
	var raw map[string]any
	_ = json.Unmarshal(out, &raw)
	if _, ok := raw["cache_control"]; !ok {
		t.Fatal(string(out))
	}
}

func TestPrepareKeepsClientCacheControl(t *testing.T) {
	body := []byte(`{"model":"claude","cache_control":{"type":"ephemeral","ttl":"1h"},"messages":[]}`)
	out, _ := Prepare(PrepInput{
		Body: body, SendAs: "anthropic/claude-sonnet-4", OrigModel: "claude",
		Kind: domain.KindOpenRouter, Provider: "Anthropic", Mode: ModeAuto,
	})
	var raw map[string]any
	_ = json.Unmarshal(out, &raw)
	cc := raw["cache_control"].(map[string]any)
	if cc["ttl"] != "1h" {
		t.Fatalf("overwrote %+v", cc)
	}
}

func TestPrepareNestedHintBlocksInject(t *testing.T) {
	body := []byte(`{"model":"qwen/qwen3-max","messages":[{"role":"user","content":[{"type":"text","text":"x","cache_control":{"type":"ephemeral"}}]}]}`)
	out, _ := Prepare(PrepInput{
		Body: body, SendAs: "qwen/qwen3-max", Kind: domain.KindOpenRouter, Mode: ModeOn,
	})
	var raw map[string]any
	_ = json.Unmarshal(out, &raw)
	if _, ok := raw["cache_control"]; ok {
		t.Fatal("nested hint still top-level inject")
	}
}

func TestPrepareOllamaNoop(t *testing.T) {
	body := []byte(`{"model":"qwen","messages":[]}`)
	out, ch := Prepare(PrepInput{Body: body, SendAs: "qwen", Kind: domain.KindOllama, Mode: ModeOn})
	if ch || string(out) != string(body) {
		t.Fatalf("ollama %s", out)
	}
}

func TestPrepareIncludeUsage(t *testing.T) {
	body := []byte(`{"model":"fast","stream":true,"messages":[]}`)
	out, ch := Prepare(PrepInput{Body: body, SendAs: "openai/gpt-4o-mini", Kind: domain.KindOpenRouter, Mode: ModeOff})
	if !ch {
		t.Fatal("include_usage")
	}
	var raw map[string]any
	_ = json.Unmarshal(out, &raw)
	so := raw["stream_options"].(map[string]any)
	if so["include_usage"] != true {
		t.Fatalf("%+v", so)
	}

	keep := []byte(`{"model":"fast","stream":true,"stream_options":{"include_usage":false},"messages":[]}`)
	out2, ch2 := Prepare(PrepInput{Body: keep, SendAs: "openai/gpt-4o-mini", Kind: domain.KindOpenRouter, Mode: ModeOff})
	_ = json.Unmarshal(out2, &raw)
	so = raw["stream_options"].(map[string]any)
	if so["include_usage"] != false {
		t.Fatalf("overwrote false %+v changed=%v", so, ch2)
	}
}

func TestHasCacheHintsTools(t *testing.T) {
	raw := map[string]any{
		"tools": []any{map[string]any{"function": map[string]any{"name": "x", "cache_control": map[string]any{"type": "ephemeral"}}}},
	}
	if !HasCacheHints(raw) {
		t.Fatal("tools")
	}
}
