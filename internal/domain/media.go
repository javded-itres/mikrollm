package domain

import "strings"

const (
	MediaChat     = "chat"
	MediaImage    = "image"
	MediaVideo    = "video"
	MediaEmbed    = "embed"
	MediaDecision = "decision"
)

func ParseMedia(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range strings.Split(s, ",") {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" || p == MediaChat {
			continue
		}
		if p != MediaImage && p != MediaVideo && p != MediaEmbed && p != MediaDecision {
			continue
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

func JoinMedia(ms []string) string {
	return strings.Join(ParseMedia(strings.Join(ms, ",")), ",")
}

func HasMedia(ms []string, kind string) bool {
	for _, m := range ms {
		if m == kind {
			return true
		}
	}
	return false
}

func MergeMedia(parts ...[]string) []string {
	var all []string
	for _, p := range parts {
		all = append(all, p...)
	}
	return ParseMedia(strings.Join(all, ","))
}

// InferMedia marks image/video models from OpenRouter output_modalities and name heuristics (LiteLLM-style).
// IsEmbeddingModel reports encoder models that answer with vectors, not text.
// Ollama publishes ModernBERT Laya as s1gnature/laya; the name is the signal
// when /api/show still lists the blob as completion.
func IsEmbeddingModel(name string) bool {
	low := strings.ToLower(strings.TrimSpace(name))
	if low == "" {
		return false
	}
	base := low
	if i := strings.IndexByte(base, ':'); i >= 0 {
		base = base[:i]
	}
	if base == "laya" || strings.HasSuffix(base, "/laya") {
		return true
	}
	for _, tok := range embeddingNameTokens {
		if strings.Contains(low, tok) {
			return true
		}
	}
	return false
}

// IsDecisionModel reports Jev and the Ollama models that speak the same
// POST /v1/systemone API (nimble, tev1) instead of chat text.
func IsDecisionModel(name string) bool {
	low := strings.ToLower(strings.TrimSpace(name))
	if low == "" {
		return false
	}
	base := low
	if i := strings.IndexByte(base, ':'); i >= 0 {
		base = base[:i]
	}
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	switch base {
	case "jev", "nimble", "tev1":
		return true
	}
	return strings.HasPrefix(base, "jev-") || strings.HasPrefix(base, "typesafe-jev")
}

func InferMedia(name string, outputModalities []string) []string {
	if IsEmbeddingModel(name) {
		return []string{MediaEmbed}
	}
	if IsDecisionModel(name) {
		return []string{MediaDecision}
	}
	var out []string
	for _, m := range outputModalities {
		switch strings.ToLower(strings.TrimSpace(m)) {
		case MediaImage, "images":
			out = append(out, MediaImage)
		case MediaVideo, "videos":
			out = append(out, MediaVideo)
		}
	}
	low := strings.ToLower(name)
	for _, tok := range imageNameTokens {
		if strings.Contains(low, tok) {
			out = append(out, MediaImage)
			break
		}
	}
	for _, tok := range videoNameTokens {
		if strings.Contains(low, tok) {
			out = append(out, MediaVideo)
			break
		}
	}
	return ParseMedia(strings.Join(out, ","))
}

func MediaLabel(ms []string) string {
	if HasMedia(ms, MediaVideo) && HasMedia(ms, MediaImage) {
		return "image+video"
	}
	if HasMedia(ms, MediaVideo) {
		return "video"
	}
	if HasMedia(ms, MediaImage) {
		return "image"
	}
	if HasMedia(ms, MediaEmbed) {
		return "embed"
	}
	if HasMedia(ms, MediaDecision) {
		return "decision"
	}
	return ""
}

// ExclusiveMedia is "video" or "image" when the model is only that kind; otherwise "".
func ExclusiveMedia(ms []string) string {
	v := HasMedia(ms, MediaVideo)
	i := HasMedia(ms, MediaImage)
	e := HasMedia(ms, MediaEmbed)
	d := HasMedia(ms, MediaDecision)
	if d && !v && !i && !e {
		return MediaDecision
	}
	if e && !v && !i && !d {
		return MediaEmbed
	}
	if v && !i && !e && !d {
		return MediaVideo
	}
	if i && !v && !e && !d {
		return MediaImage
	}
	return ""
}

var imageNameTokens = []string{
	"dall-e", "dall_e", "dalle", "gpt-image", "flux", "imagen", "recraft", "ideogram",
	"sdxl", "stable-diffusion", "stable_diffusion", "hidream", "grok-imagine",
	"seedream", "photon", "flash-image", "pro-image", "image-preview", "nanobanana",
	"z-image", "z_image",
}

var embeddingNameTokens = []string{
	"embed", "/laya", "laya:", ":laya", "modern-bert", "modernbert",
	"bge-", "bge/", "minilm", "nomic-embed", "snowflake-arctic-embed",
}

var videoNameTokens = []string{
	"sora", "kling", "runway", "luma-dream", "luma/dream", "dream-machine",
	"wan-2", "wan2", "wan/", "minimax-video", "hailuo", "veo-", "veo2", "veo3",
	"hunyuan-video", "ltx-video", "seedance", "sora-2", "grok-imagine-video",
	"imagine-video", "/veo",
}
