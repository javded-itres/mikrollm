package domain

import "strings"

// GenerationParam is a public control a client may send on image or video generation.
// OpenComfy models publish their own list. Providers that do not (OpenRouter and
// similar) get this fallback so the client can still pass seed, size, and a reference.
type GenerationParam struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Description string `json:"description,omitempty"`
	Source      string `json:"source,omitempty"`
}

// FallbackGenerationParams is used when the upstream model card has no parameters.
// Source is "fallback": the gateway forwards the fields, and the provider may ignore them.
func FallbackGenerationParams(alias, upstream string, media []string) []GenerationParam {
	id := strings.ToLower(alias + " " + upstream)
	kind := ""
	if HasMedia(media, MediaVideo) || strings.Contains(id, "video") || strings.Contains(id, "hailuo") || strings.Contains(id, "seedance") || strings.Contains(id, "sora") {
		kind = MediaVideo
	} else if HasMedia(media, MediaImage) || strings.Contains(id, "image") || strings.Contains(id, "flux") {
		kind = MediaImage
	}
	switch kind {
	case MediaImage:
		return []GenerationParam{
			{Name: "prompt", Type: "string", Required: true, Description: "What to draw.", Source: "fallback"},
			{Name: "size", Type: "string", Description: "WIDTHxHEIGHT such as 1024x1024. The provider may ignore it.", Source: "fallback"},
			{Name: "seed", Type: "integer", Description: "Keep this value when editing the same image. The provider may ignore it.", Source: "fallback"},
			{Name: "input_image", Type: "image", Description: "Reference image. Send the previous file when the user asks to change it.", Source: "fallback"},
		}
	case MediaVideo:
		return []GenerationParam{
			{Name: "prompt", Type: "string", Required: true, Description: "What the clip shows.", Source: "fallback"},
			{Name: "seconds", Type: "integer", Description: "Duration in seconds. The provider may ignore it.", Source: "fallback"},
			{Name: "seed", Type: "integer", Description: "Keep this value when revising the same clip. The provider may ignore it.", Source: "fallback"},
			{Name: "input_image", Type: "image", Description: "Source still, when the model can start from an image.", Source: "fallback"},
		}
	default:
		return nil
	}
}
