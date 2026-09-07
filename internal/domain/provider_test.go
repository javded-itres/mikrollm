package domain

import "testing"

func TestProviderOf(t *testing.T) {
	if g := ProviderOf("openai/gpt-4o-mini", "openrouter"); g != "OpenAI" {
		t.Fatalf("got %q", g)
	}
	if g := ProviderOf("anthropic/claude-sonnet-4", KindOpenRouter); g != "Anthropic" {
		t.Fatalf("got %q", g)
	}
	if g := ProviderOf("llama3.2", KindOllama); g != "Ollama" {
		t.Fatalf("got %q", g)
	}
	if g := ProviderOf("qwen3", KindLMStudio); g != "LM Studio" {
		t.Fatalf("got %q", g)
	}
	if g := ProviderOf("x-ai/grok-4", KindOpenRouter); g != "xAI" {
		t.Fatalf("got %q", g)
	}
}

func TestPerMillionAndPriceLabel(t *testing.T) {
	if PerMillion(0.00001) != 10 {
		t.Fatalf("10/1M got %v", PerMillion(0.00001))
	}
	if PriceLabel(false, 0, 0) != "" {
		t.Fatal("unpriced")
	}
	if PriceLabel(true, 0, 0) != "бесплатно" {
		t.Fatal("free")
	}
	if g := PriceLabel(true, 10, 50); g != "$10 / $50" {
		t.Fatalf("got %q", g)
	}
	if PriceBand(true, 0) != "free" || PriceBand(false, 0) != "none" || PriceBand(true, 3) != "lt10" {
		t.Fatal("band")
	}
}
