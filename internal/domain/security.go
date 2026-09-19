package domain

import "encoding/json"

const (
	GuardSystemPrompt = "system_prompt"
	GuardBlockWords   = "block_words"
	GuardRegex        = "regex"
	GuardPII          = "pii"
	GuardInjection    = "prompt_injection"
	GuardCategory     = "category"
	GuardNSFW         = "nsfw"

	GuardBlock = "block"
	GuardMask  = "mask"

	GuardPre  = "pre"
	GuardPost = "post"
	GuardBoth = "both"

	GuardTargetAlias = "alias"
	GuardTargetQueue = "queue"
	GuardTargetModel = "model"
)

type PolicyConfig struct {
	Prompt     string   `json:"prompt,omitempty"`
	Words      []string `json:"words,omitempty"`
	Pattern    string   `json:"pattern,omitempty"`
	PII        []string `json:"pii,omitempty"`
	Categories []string `json:"categories,omitempty"`
	Plugins    []string `json:"plugins,omitempty"`
}

type PolicyTarget struct {
	Kind string `json:"kind"`
	Key  string `json:"key"`
}

type Policy struct {
	ID      int64          `json:"id"`
	Name    string         `json:"name"`
	Kind    string         `json:"kind"`
	Action  string         `json:"action"`
	Mode    string         `json:"mode"`
	Enabled bool           `json:"enabled"`
	Config  PolicyConfig   `json:"config"`
	Targets []PolicyTarget `json:"targets,omitempty"`
}

func (p Policy) ConfigJSON() string {
	b, _ := json.Marshal(p.Config)
	if len(b) == 0 {
		return "{}"
	}
	return string(b)
}

func ParsePolicyConfig(s string) PolicyConfig {
	var c PolicyConfig
	if s == "" {
		return c
	}
	_ = json.Unmarshal([]byte(s), &c)
	return c
}

func NormalizeGuardKind(k string) string {
	switch k {
	case GuardSystemPrompt, GuardBlockWords, GuardRegex, GuardPII, GuardInjection, GuardCategory, GuardNSFW:
		return k
	default:
		return GuardBlockWords
	}
}

func NormalizeGuardAction(a string) string {
	if a == GuardMask {
		return GuardMask
	}
	return GuardBlock
}

func NormalizeGuardMode(m string) string {
	switch m {
	case GuardPost, GuardBoth:
		return m
	default:
		return GuardPre
	}
}

func GuardKindLabel(k string) string {
	switch k {
	case GuardSystemPrompt:
		return "Системный промпт"
	case GuardBlockWords:
		return "Стоп-слова"
	case GuardRegex:
		return "Регулярное выражение"
	case GuardPII:
		return "ПДн / PII"
	case GuardInjection:
		return "Prompt injection"
	case GuardCategory:
		return "Категории (плагины)"
	case GuardNSFW:
		return "NSFW / 18+"
	default:
		return k
	}
}
