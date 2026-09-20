package domain

import (
	"fmt"
	"strconv"
	"strings"
)

var providerNames = map[string]string{
	"openai":       "OpenAI",
	"anthropic":    "Anthropic",
	"google":       "Google",
	"x-ai":         "xAI",
	"xai":          "xAI",
	"meta":         "Meta",
	"meta-llama":   "Meta",
	"mistralai":    "Mistral",
	"mistral":      "Mistral",
	"qwen":         "Qwen",
	"deepseek":     "DeepSeek",
	"cohere":       "Cohere",
	"perplexity":   "Perplexity",
	"nvidia":       "NVIDIA",
	"microsoft":    "Microsoft",
	"amazon":       "Amazon",
	"ai21":         "AI21",
	"together":     "Together",
	"groq":         "Groq",
	"fireworks":    "Fireworks",
	"huggingface":  "Hugging Face",
	"minimax":      "MiniMax",
	"z-ai":         "Z.ai",
	"zhipu":        "Zhipu",
	"moonshotai":   "Moonshot",
	"moonshot":     "Moonshot",
	"inclusionai":  "InclusionAI",
	"liquid":       "Liquid",
	"nousresearch": "Nous",
	"inflection":   "Inflection",
	"openrouter":   "OpenRouter",
	"ollama":       "Ollama",
	"opencomfy":    "OpenComfy",
}

func ProviderOf(model, kind string) string {
	model = strings.TrimSpace(model)
	if i := strings.Index(model, "/"); i > 0 {
		return ProviderLabel(model[:i])
	}
	return Backend{Kind: kind}.Label()
}

func ProviderLabel(slug string) string {
	slug = strings.TrimSpace(strings.ToLower(slug))
	if slug == "" {
		return ""
	}
	if n, ok := providerNames[slug]; ok {
		return n
	}
	parts := strings.FieldsFunc(slug, func(r rune) bool { return r == '-' || r == '_' })
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

func PerMillion(perToken float64) float64 {
	if perToken <= 0 {
		return 0
	}
	return perToken * 1_000_000
}

func FormatUSD(n float64) string {
	if n <= 0 {
		return "$0"
	}
	if n >= 1 {
		if n == float64(int(n)) {
			return fmt.Sprintf("$%.0f", n)
		}
		return fmt.Sprintf("$%.2f", n)
	}
	prec := 2
	if n < 0.01 {
		prec = 4
	} else if n < 0.1 {
		prec = 3
	}
	s := strconv.FormatFloat(n, 'f', prec, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if !strings.Contains(s, ".") {
		s += ".00"
	} else if i := strings.IndexByte(s, '.'); len(s)-i-1 == 1 {
		s += "0"
	}
	return "$" + s
}

func PriceLabel(priced bool, promptPerM, completionPerM float64) string {
	return FormatCatalogPrice(priced, promptPerM, completionPerM, 0, 0, 0)
}

// FormatCatalogPrice: chat $/1M in/out, image per кадр or /1M img-токен, video /сек.
func FormatCatalogPrice(priced bool, promptPerM, completionPerM, imagePerImg, imagePerM, videoPerSec float64) string {
	var parts []string
	if promptPerM > 0 || completionPerM > 0 {
		parts = append(parts, FormatUSD(promptPerM)+" / "+FormatUSD(completionPerM))
	}
	if imagePerImg > 0 {
		parts = append(parts, FormatUSD(imagePerImg)+" / кадр")
	} else if imagePerM > 0 {
		parts = append(parts, FormatUSD(imagePerM)+" / 1M img")
	}
	if videoPerSec > 0 {
		parts = append(parts, FormatUSD(videoPerSec)+" / сек")
	}
	if len(parts) > 0 {
		return strings.Join(parts, " · ")
	}
	if priced {
		return "бесплатно"
	}
	return ""
}

func PriceBandValue(promptPerM, imagePerImg, imagePerM, videoPerSec float64) float64 {
	if promptPerM > 0 {
		return promptPerM
	}
	if imagePerM > 0 {
		return imagePerM
	}
	if imagePerImg > 0 {
		return imagePerImg * 1000
	}
	if videoPerSec > 0 {
		return videoPerSec * 100
	}
	return 0
}

func PriceBand(priced bool, promptPerM float64) string {
	if !priced {
		return "none"
	}
	if promptPerM <= 0 {
		return "free"
	}
	if promptPerM < 0.5 {
		return "lt0.5"
	}
	if promptPerM < 2 {
		return "lt2"
	}
	if promptPerM < 10 {
		return "lt10"
	}
	return "more"
}
