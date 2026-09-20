package domain

import (
	"strings"
	"testing"
)

func TestNormalizeKind(t *testing.T) {
	cases := map[string]string{
		"":             KindOllama,
		"Ollama":       KindOllama,
		"vllm":         KindVLLM,
		"v-llm":        KindVLLM,
		"lmstudio":     KindLMStudio,
		"LM Studio":    KindLMStudio,
		"lm-studio":    KindLMStudio,
		"lms":          KindLMStudio,
		"llstudio":     KindLMStudio,
		"openrouter":   KindOpenRouter,
		"open-router":  KindOpenRouter,
		"ollama-cloud": KindOllamaCloud,
		"ollamacloud":  KindOllamaCloud,
		"opencomfy":    KindOpenComfy,
		"open-comfy":   KindOpenComfy,
		"comfyui":      KindOpenComfy,
	}
	for in, want := range cases {
		if g := NormalizeKind(in); g != want {
			t.Errorf("NormalizeKind(%q)=%q want %q", in, g, want)
		}
	}
}

func TestBackendCaps(t *testing.T) {
	ollama := Backend{Kind: "ollama"}
	vllm := Backend{Kind: "vllm"}
	lms := Backend{Kind: "lmstudio"}

	if !ollama.CanLoad() || !ollama.CanPull() || !ollama.CanDelete() {
		t.Fatal("ollama caps")
	}
	if lms.CanDelete() || !lms.CanLoad() || !lms.CanPull() {
		t.Fatal("lmstudio caps")
	}
	if vllm.CanLoad() || vllm.CanPull() || vllm.CanDelete() {
		t.Fatal("vllm must not load/pull/delete via API")
	}
	if vllm.ChatPath() != "/v1/chat/completions" || ollama.ChatPath() != "/api/chat" {
		t.Fatal("chat path")
	}
	if vllm.Label() != "vLLM" || lms.Label() != "LM Studio" {
		t.Fatal("labels")
	}
	if vllm.LoadHint() == "" || !strings.Contains(vllm.LoadHint(), "vllm serve") {
		t.Fatalf("hint %q", vllm.LoadHint())
	}
	if ollama.LoadHint() != "" {
		t.Fatal("ollama hint")
	}

	or := Backend{Kind: "openrouter"}
	cloud := Backend{Kind: "ollama-cloud"}
	if or.CanLoad() || or.CanPull() || !or.Cloud() || !RequiresToken(or.Kind) {
		t.Fatal("openrouter caps")
	}
	if cloud.CanLoad() || cloud.CanDelete() || !cloud.NativeOllama() || !cloud.Cloud() {
		t.Fatal("ollama-cloud caps")
	}
	if or.OpenAIChatPath() != "/chat/completions" || cloud.OpenAIChatPath() != "/v1/chat/completions" {
		t.Fatal("openai chat path")
	}
	if CanonicalBaseURL("openrouter", "") != DefaultOpenRouterURL {
		t.Fatal("openrouter default url")
	}
	if CanonicalBaseURL("openrouter", "https://openrouter.ai/") != DefaultOpenRouterURL {
		t.Fatal("openrouter host only")
	}
	if CanonicalBaseURL("ollama-cloud", "") != DefaultOllamaCloudURL {
		t.Fatal("ollama-cloud default url")
	}

	oc := Backend{Kind: "opencomfy"}
	if oc.CanLoad() || oc.CanPull() || oc.Cloud() {
		t.Fatal("opencomfy caps")
	}
	if !RequiresToken(oc.Kind) || oc.Label() != "OpenComfy" || oc.DefaultPort() != "8788" {
		t.Fatal("opencomfy token/label/port")
	}
	if oc.OpenAIChatPath() != "/v1/chat/completions" || oc.ModelsPath() != "/v1/models" {
		t.Fatal("opencomfy paths")
	}
	if CanonicalBaseURL("opencomfy", "http://192.168.88.252:8788/v1/") != "http://192.168.88.252:8788" {
		t.Fatalf("opencomfy trim /v1: %q", CanonicalBaseURL("opencomfy", "http://192.168.88.252:8788/v1/"))
	}
}

func TestSanitizeBackendURL(t *testing.T) {
	ok, err := SanitizeBackendURL("ollama", "http://192.168.88.82:11434/")
	if err != nil || ok != "http://192.168.88.82:11434" {
		t.Fatalf("lan %q %v", ok, err)
	}
	if _, err := SanitizeBackendURL("ollama", "file:///etc/passwd"); err == nil {
		t.Fatal("file")
	}
	if _, err := SanitizeBackendURL("ollama", "http://user:pass@evil"); err == nil {
		t.Fatal("userinfo")
	}
	if _, err := SanitizeBackendURL("vllm", "javascript:alert(1)"); err == nil {
		t.Fatal("js")
	}
	got, err := SanitizeBackendURL("openrouter", "")
	if err != nil || got != DefaultOpenRouterURL {
		t.Fatalf("openrouter default %q %v", got, err)
	}
	got, err = SanitizeBackendURL("opencomfy", "http://192.168.88.252:8788/v1/")
	if err != nil || got != "http://192.168.88.252:8788" {
		t.Fatalf("opencomfy url %q %v", got, err)
	}
}

func TestSanitizeToken(t *testing.T) {
	cases := map[string]string{
		"  sk-or-v1-abc  ":      "sk-or-v1-abc",
		"Bearer sk-or-v1-abc":   "sk-or-v1-abc",
		"bearer sk-or-v1-abc":   "sk-or-v1-abc",
		`"sk-or-v1-abc"`:        "sk-or-v1-abc",
		"Bearer sk-or-v1-abc\n": "sk-or-v1-abc",
		"sk-or-v1-abc":          "sk-or-v1-abc",
	}
	for in, want := range cases {
		if g := SanitizeToken(in); g != want {
			t.Errorf("SanitizeToken(%q)=%q want %q", in, g, want)
		}
	}
}

func TestFormatContext(t *testing.T) {
	if FormatContext(0) != "" || FormatContext(32768) != "32k" {
		t.Fatalf("got %q %q", FormatContext(0), FormatContext(32768))
	}
	if FormatContext(128000) != "128k" {
		t.Fatalf("128000 %q", FormatContext(128000))
	}
}
