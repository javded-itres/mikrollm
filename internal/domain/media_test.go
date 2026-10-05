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
	if !HasMedia(InferMedia("s1gnature/laya:latest", nil), MediaEmbed) {
		t.Fatal("laya")
	}
	if ExclusiveMedia(InferMedia("s1gnature/laya", nil)) != MediaEmbed {
		t.Fatal("laya exclusive")
	}
	if HasMedia(InferMedia("s1gnature/laya", nil), MediaImage) || HasMedia(InferMedia("nomic-embed-text", nil), MediaChat) {
		t.Fatal("embed mixed with chat or image")
	}
	if ExclusiveMedia(InferMedia("minimax-hailuo-02", nil)) != MediaVideo {
		t.Fatal("hailuo")
	}
	if ExclusiveMedia([]string{MediaImage}) != MediaImage {
		t.Fatal("image")
	}
	if ExclusiveMedia(nil) != "" {
		t.Fatal("chat")
	}
	for _, name := range []string{"jev-latest", "jev-1.13.0", "typesafe-jev-1.13.0", "typesafe/jev-1.13", "nimble", "tev1:0.8b"} {
		if ExclusiveMedia(InferMedia(name, nil)) != MediaDecision {
			t.Fatal(name)
		}
	}
	if IsDecisionModel("jeffrey") || IsDecisionModel("jevembed-qwen3") {
		t.Fatal("not a decision model")
	}
	if ExclusiveMedia(InferMedia("jevembed-qwen3", nil)) != MediaEmbed {
		t.Fatal("jevembed stays embed")
	}
}
