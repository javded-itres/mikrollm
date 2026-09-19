package promptcache

import "testing"

func TestEstimateSavedDiscountWins(t *testing.T) {
	u := Usage{HasDiscount: true, CacheDiscount: -0.2, CachedTokens: 100, Cost: 9, HasCost: true}
	got, ok := EstimateSaved(u, 3, "Anthropic")
	if !ok || got != -0.2 {
		t.Fatalf("%v %v", got, ok)
	}
}

func TestEstimateSavedMultiplierOpenAI(t *testing.T) {
	u := Usage{CachedTokens: 1_000_000}
	got, ok := EstimateSaved(u, 1, "OpenAI")
	if !ok {
		t.Fatal("ok")
	}
	// 1 USD/1M * 1e6 * (1-0.50) = 0.50
	if got != 0.5 {
		t.Fatalf("got %v", got)
	}
}

func TestEstimateSavedIgnoresCost(t *testing.T) {
	u := Usage{CachedTokens: 1000, HasCost: true, Cost: 99, PromptTokens: 1000}
	got, ok := EstimateSaved(u, 1, "Anthropic")
	if !ok || got == 99 {
		t.Fatalf("used cost: %v %v", got, ok)
	}
}

func TestEstimateSavedUnknownProvider(t *testing.T) {
	u := Usage{CachedTokens: 100}
	if _, ok := EstimateSaved(u, 1, "Acme"); ok {
		t.Fatal("unknown")
	}
}
