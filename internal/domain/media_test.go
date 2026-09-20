package domain

import "testing"

func TestInferMedia(t *testing.T) {
	if !HasMedia(InferMedia("black-forest-labs/flux.2-pro", nil), MediaImage) {
		t.Fatal("flux")
	}
	if !HasMedia(InferMedia("openai/sora-2", nil), MediaVideo) {
		t.Fatal("sora")
	}
	if !HasMedia(InferMedia("google/gemini-2.5-flash-image-preview", []string{"text", "image"}), MediaImage) {
		t.Fatal("gemini image")
	}
	if HasMedia(InferMedia("ornith-1.5:35b", []string{"text"}), MediaImage) {
		t.Fatal("text-only")
	}
}

func TestExclusiveMedia(t *testing.T) {
	if ExclusiveMedia(InferMedia("minimax-hailuo-02", nil)) != MediaVideo {
		t.Fatal("hailuo")
	}
	if ExclusiveMedia([]string{MediaImage}) != MediaImage {
		t.Fatal("image")
	}
	if ExclusiveMedia(nil) != "" {
		t.Fatal("chat")
	}
}
