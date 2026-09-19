package domain

import "strings"

const (
	MediaChat  = "chat"
	MediaImage = "image"
	MediaVideo = "video"
)

func ParseMedia(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range strings.Split(s, ",") {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" || p == MediaChat {
			continue
		}
		if p != MediaImage && p != MediaVideo {
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
func InferMedia(name string, outputModalities []string) []string {
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
	return ""
}

var imageNameTokens = []string{
	"dall-e", "dall_e", "dalle", "gpt-image", "flux", "imagen", "recraft", "ideogram",
	"sdxl", "stable-diffusion", "stable_diffusion", "hidream", "grok-imagine",
	"seedream", "photon", "flash-image", "pro-image", "image-preview", "nanobanana",
	"z-image", "z_image",
}

var videoNameTokens = []string{
	"sora", "kling", "runway", "luma-dream", "luma/dream", "dream-machine",
	"wan-2", "wan2", "wan/", "minimax-video", "hailuo", "veo-", "veo2", "veo3",
	"hunyuan-video", "ltx-video", "seedance", "sora-2", "grok-imagine-video",
	"imagine-video", "/veo",
}
