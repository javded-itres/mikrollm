package domain

import (
	"net/http"
	"strings"
)

const UpstreamUserAgent = "MikroLLM/0.0.1"

// SanitizeToken strips whitespace, quotes and a leading "Bearer " so a pasted
// curl header is stored as the raw key.
func SanitizeToken(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"'`)
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if len(s) >= 6 && strings.EqualFold(s[:6], "bearer") {
		s = strings.TrimSpace(s[6:])
	}
	return s
}

func ApplyUpstreamHeaders(h http.Header, b Backend) {
	if tok := SanitizeToken(b.Token); tok != "" {
		h.Set("Authorization", "Bearer "+tok)
	}
	if h.Get("User-Agent") == "" {
		h.Set("User-Agent", UpstreamUserAgent)
	}
	if b.KindNorm() == KindOpenRouter {
		h.Set("HTTP-Referer", "https://github.com/javded-itres/mikrollm")
		h.Set("X-Title", "MikroLLM")
		h.Set("X-OpenRouter-Title", "MikroLLM")
	}
}
