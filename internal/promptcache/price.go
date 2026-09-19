package promptcache

import (
	"strings"

	"github.com/javded-itres/mikrollm/internal/domain"
)

func ReadMultiplier(provider string) (float64, bool) {
	p := strings.ToLower(strings.TrimSpace(provider))
	if p == "" {
		return 0, false
	}
	if i := strings.Index(p, "/"); i > 0 {
		p = p[:i]
	}
	label := strings.ToLower(domain.ProviderLabel(p))
	switch p {
	case "anthropic":
		return 0.10, true
	case "deepseek":
		return 0.10, true
	case "qwen":
		return 0.10, true
	case "google":
		return 0.25, true
	case "x-ai", "xai", "grok":
		return 0.25, true
	case "moonshotai", "moonshot":
		return 0.25, true
	case "groq":
		return 0.50, true
	case "openai":
		return 0.50, true
	case "z-ai", "zhipu":
		return 0.20, true
	}
	switch label {
	case "anthropic":
		return 0.10, true
	case "deepseek":
		return 0.10, true
	case "qwen":
		return 0.10, true
	case "google":
		return 0.25, true
	case "xai":
		return 0.25, true
	case "moonshot":
		return 0.25, true
	case "groq":
		return 0.50, true
	case "openai":
		return 0.50, true
	case "z.ai", "zhipu":
		return 0.20, true
	default:
		return 0, false
	}
}

func writeMult(provider string) float64 {
	m, ok := ReadMultiplier(provider)
	if !ok {
		return 1
	}
	p := strings.ToLower(strings.TrimSpace(provider))
	label := strings.ToLower(domain.ProviderLabel(p))
	if strings.HasPrefix(p, "anthropic") || label == "anthropic" || strings.HasPrefix(p, "qwen") || label == "qwen" {
		return 1.25
	}
	_ = m
	return 1
}

func EstimateSaved(u Usage, promptUSDPer1M float64, provider string) (float64, bool) {
	if u.HasDiscount {
		return u.CacheDiscount, true
	}
	if u.CachedTokens == 0 && u.CacheWriteTokens == 0 {
		return 0, false
	}
	if promptUSDPer1M <= 0 {
		return 0, false
	}
	readMult, known := ReadMultiplier(provider)
	if !known {
		return 0, false
	}
	saved := promptUSDPer1M / 1e6 * float64(u.CachedTokens) * (1 - readMult)
	if u.CacheWriteTokens > 0 {
		wm := writeMult(provider)
		if wm > 1 {
			saved -= promptUSDPer1M / 1e6 * float64(u.CacheWriteTokens) * (wm - 1)
		}
	}
	return saved, true
}
