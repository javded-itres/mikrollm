package domain

import "testing"

func TestFallbackGenerationParamsImageHasSeed(t *testing.T) {
	got := FallbackGenerationParams("openrouter/flux-schnell", "", []string{MediaImage})
	names := map[string]bool{}
	for _, p := range got {
		names[p.Name] = true
		if p.Source != "fallback" {
			t.Fatalf("source %s", p.Source)
		}
	}
	for _, want := range []string{"prompt", "size", "seed", "input_image"} {
		if !names[want] {
			t.Fatalf("missing %s in %+v", want, got)
		}
	}
}

func TestFallbackGenerationParamsChatEmpty(t *testing.T) {
	if got := FallbackGenerationParams("gpt-4", "", nil); got != nil {
		t.Fatalf("chat model %+v", got)
	}
}
