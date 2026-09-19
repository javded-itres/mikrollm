package guard

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/javded-itres/mikrollm/internal/domain"
)

type Block struct {
	Policy  string
	Kind    string
	Message string
}

type Result struct {
	Body    []byte
	Block   *Block
	Changed bool
}

func Dedup(ps []domain.Policy) []domain.Policy {
	seen := map[int64]bool{}
	out := make([]domain.Policy, 0, len(ps))
	for _, p := range ps {
		if p.ID != 0 && seen[p.ID] {
			continue
		}
		if p.ID != 0 {
			seen[p.ID] = true
		}
		if !p.Enabled {
			continue
		}
		out = append(out, p)
	}
	return out
}

func HasPost(ps []domain.Policy) bool {
	for _, p := range Dedup(ps) {
		if p.Kind == domain.GuardSystemPrompt {
			continue
		}
		if Applies(p, domain.GuardPost) {
			return true
		}
	}
	return false
}

func Applies(p domain.Policy, phase string) bool {
	if p.Kind == domain.GuardSystemPrompt {
		return phase == domain.GuardPre
	}
	switch p.Mode {
	case domain.GuardBoth:
		return true
	case domain.GuardPost:
		return phase == domain.GuardPost
	default:
		return phase == domain.GuardPre
	}
}

func Apply(body []byte, policies []domain.Policy, phase string) Result {
	policies = Dedup(policies)
	need := false
	for _, p := range policies {
		if Applies(p, phase) {
			need = true
			break
		}
	}
	if !need {
		return Result{Body: body}
	}
	raw, ok := decode(body)
	if !ok {
		return Result{Body: body}
	}
	changed := false
	if phase == domain.GuardPre {
		if prompt := joinSystem(policies); prompt != "" {
			injectSystem(raw, prompt)
			changed = true
		}
	}
	for _, p := range policies {
		if !Applies(p, phase) || p.Kind == domain.GuardSystemPrompt {
			continue
		}
		blocked, ch := applyPolicy(raw, p)
		if blocked != nil {
			return Result{Body: body, Block: blocked}
		}
		changed = changed || ch
	}
	if !changed {
		return Result{Body: body}
	}
	out, err := json.Marshal(raw)
	if err != nil {
		return Result{Body: body}
	}
	return Result{Body: out, Changed: true}
}

func joinSystem(ps []domain.Policy) string {
	var parts []string
	seen := map[string]bool{}
	for _, p := range ps {
		if p.Kind != domain.GuardSystemPrompt || !Applies(p, domain.GuardPre) {
			continue
		}
		s := strings.TrimSpace(p.Config.Prompt)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		parts = append(parts, s)
	}
	return strings.Join(parts, "\n\n")
}

func applyPolicy(raw map[string]any, p domain.Policy) (*Block, bool) {
	switch p.Kind {
	case domain.GuardBlockWords:
		return walk(raw, p, func(s string) (string, *Block) {
			for _, w := range p.Config.Words {
				w = strings.TrimSpace(w)
				if w == "" {
					continue
				}
				if containsFold(s, w) {
					if p.Action == domain.GuardMask {
						return maskFold(s, w, "[filtered]"), nil
					}
					return s, reject(p, "стоп-слово")
				}
			}
			return s, nil
		})
	case domain.GuardRegex:
		re, err := compileUser(p.Config.Pattern)
		if err != nil || re == nil {
			return nil, false
		}
		return walk(raw, p, func(s string) (string, *Block) {
			if !re.MatchString(s) {
				return s, nil
			}
			if p.Action == domain.GuardMask {
				return re.ReplaceAllString(s, "[filtered]"), nil
			}
			return s, reject(p, "регулярное выражение")
		})
	case domain.GuardPII:
		return walk(raw, p, func(s string) (string, *Block) {
			ns, hit := applyPII(s, p.Config.PII, p.Action == domain.GuardMask)
			if hit && p.Action != domain.GuardMask {
				return s, reject(p, "ПДн")
			}
			return ns, nil
		})
	case domain.GuardInjection:
		phrases := append([]string{}, injectionPhrases...)
		for _, w := range p.Config.Words {
			if t := strings.TrimSpace(w); t != "" {
				phrases = append(phrases, t)
			}
		}
		return walk(raw, p, func(s string) (string, *Block) {
			if injectionHit(s, phrases) {
				if p.Action == domain.GuardMask {
					return "[filtered]", nil
				}
				return s, reject(p, "prompt injection")
			}
			return s, nil
		})
	case domain.GuardCategory, domain.GuardNSFW:
		ids := selectedPlugins(p.Kind, p.Config.Plugins, p.Config.Categories)
		if p.Kind == domain.GuardCategory && len(ids) == 0 {
			ids = []string{"violence", "self_harm", "csam", "hate", "weapons", "drugs"}
		}
		extra := p.Config.Words
		return walk(raw, p, func(s string) (string, *Block) {
			if cat, ok := MatchPlugins(s, ids); ok {
				if p.Action == domain.GuardMask {
					return "[filtered]", nil
				}
				return s, reject(p, "плагин "+cat)
			}
			low := strings.ToLower(s)
			for _, w := range extra {
				if keywordHit(low, w) {
					if p.Action == domain.GuardMask {
						return "[filtered]", nil
					}
					return s, reject(p, "плагин extra")
				}
			}
			return s, nil
		})
	default:
		return nil, false
	}
}

func reject(p domain.Policy, why string) *Block {
	return &Block{
		Policy:  p.Name,
		Kind:    p.Kind,
		Message: "Отклонено фильтром «" + p.Name + "» (" + why + ").",
	}
}

func decode(body []byte) (map[string]any, bool) {
	var raw map[string]any
	if json.Unmarshal(body, &raw) != nil || raw == nil {
		return nil, false
	}
	return raw, true
}

func injectSystem(raw map[string]any, prompt string) {
	msgs, _ := raw["messages"].([]any)
	if len(msgs) > 0 {
		if m, ok := msgs[0].(map[string]any); ok {
			if role, _ := m["role"].(string); strings.EqualFold(role, "system") {
				switch c := m["content"].(type) {
				case string:
					m["content"] = prompt + "\n\n" + c
				case []any:
					part := map[string]any{"type": "text", "text": prompt}
					m["content"] = append([]any{part}, c...)
				default:
					m["content"] = prompt + "\n\n" + contentText(c)
				}
				msgs[0] = m
				raw["messages"] = msgs
				return
			}
		}
	}
	sys := map[string]any{"role": "system", "content": prompt}
	raw["messages"] = append([]any{sys}, msgs...)
}

func walk(raw map[string]any, p domain.Policy, fn func(string) (string, *Block)) (*Block, bool) {
	changed := false
	if msgs, ok := raw["messages"].([]any); ok {
		for i, item := range msgs {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			next, blk := rewriteContent(m["content"], fn)
			if blk != nil {
				return blk, false
			}
			if !contentEqual(m["content"], next) {
				m["content"] = next
				msgs[i] = m
				changed = true
			}
		}
		if changed {
			raw["messages"] = msgs
		}
	}
	if prompt, ok := raw["prompt"].(string); ok {
		next, blk := fn(prompt)
		if blk != nil {
			return blk, false
		}
		if next != prompt {
			raw["prompt"] = next
			changed = true
		}
	}
	if blk, ch := phaseChoices(raw, fn); blk != nil {
		return blk, false
	} else if ch {
		changed = true
	}
	_ = p
	return nil, changed
}

func phaseChoices(raw map[string]any, fn func(string) (string, *Block)) (*Block, bool) {
	choices, ok := raw["choices"].([]any)
	changed := false
	if ok {
		for i, c := range choices {
			cm, ok := c.(map[string]any)
			if !ok {
				continue
			}
			if msg, ok := cm["message"].(map[string]any); ok {
				next, blk := rewriteContent(msg["content"], fn)
				if blk != nil {
					return blk, false
				}
				if !contentEqual(msg["content"], next) {
					msg["content"] = next
					cm["message"] = msg
					choices[i] = cm
					changed = true
				}
			}
			if t, ok := cm["text"].(string); ok {
				next, blk := fn(t)
				if blk != nil {
					return blk, false
				}
				if next != t {
					cm["text"] = next
					choices[i] = cm
					changed = true
				}
			}
		}
		if changed {
			raw["choices"] = choices
		}
	}
	if msg, ok := raw["message"].(map[string]any); ok {
		next, blk := rewriteContent(msg["content"], fn)
		if blk != nil {
			return blk, false
		}
		if !contentEqual(msg["content"], next) {
			msg["content"] = next
			raw["message"] = msg
			changed = true
		}
	}
	return nil, changed
}

func rewriteContent(content any, fn func(string) (string, *Block)) (any, *Block) {
	switch t := content.(type) {
	case string:
		return fn(t)
	case []any:
		out := make([]any, len(t))
		copy(out, t)
		for i, p := range out {
			m, ok := p.(map[string]any)
			if !ok {
				continue
			}
			if s, ok := m["text"].(string); ok {
				ns, blk := fn(s)
				if blk != nil {
					return content, blk
				}
				if ns != s {
					nm := map[string]any{}
					for k, v := range m {
						nm[k] = v
					}
					nm["text"] = ns
					out[i] = nm
				}
			}
		}
		return out, nil
	default:
		s := contentText(content)
		if s == "" {
			return content, nil
		}
		ns, blk := fn(s)
		if blk != nil {
			return content, blk
		}
		return ns, nil
	}
}

func contentText(content any) string {
	switch t := content.(type) {
	case string:
		return t
	case []any:
		var b strings.Builder
		for _, p := range t {
			if m, ok := p.(map[string]any); ok {
				if s, ok := m["text"].(string); ok {
					if b.Len() > 0 {
						b.WriteByte(' ')
					}
					b.WriteString(s)
				}
			}
		}
		return b.String()
	default:
		return ""
	}
}

func contentEqual(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

func maskFold(s, word, repl string) string {
	re, err := regexp.Compile("(?i)" + regexp.QuoteMeta(word))
	if err != nil {
		return s
	}
	return re.ReplaceAllString(s, repl)
}

func compileUser(pat string) (*regexp.Regexp, error) {
	pat = strings.TrimSpace(pat)
	if pat == "" || len(pat) > 256 {
		return nil, nil
	}
	return regexp.Compile("(?i)" + pat)
}

func injectionHit(s string, phrases []string) bool {
	low := strings.ToLower(s)
	for _, p := range phrases {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" && strings.Contains(low, p) {
			return true
		}
	}
	return injectionHeuristic(low)
}

func injectionHeuristic(low string) bool {
	verbs := []string{"ignore", "disregard", "forget", "bypass", "override", "игнорируй", "забудь", "обойди"}
	objs := []string{"instruction", "prompt", "system", "rules", "инструкц", "промпт", "правил"}
	for _, v := range verbs {
		i := strings.Index(low, v)
		if i < 0 {
			continue
		}
		window := low[i:]
		if len(window) > 80 {
			window = window[:80]
		}
		for _, o := range objs {
			if strings.Contains(window, o) {
				return true
			}
		}
	}
	return false
}

func applyPII(s string, kinds []string, mask bool) (string, bool) {
	if len(kinds) == 0 {
		kinds = []string{"email", "phone", "card"}
	}
	hit := false
	out := s
	for _, k := range kinds {
		re := piiRe[k]
		if re == nil {
			continue
		}
		if re.MatchString(out) {
			hit = true
			if mask {
				out = re.ReplaceAllString(out, "["+k+"]")
			}
		}
	}
	return out, hit
}

func IsStream(body []byte) bool {
	raw, ok := decode(body)
	if !ok {
		return false
	}
	switch t := raw["stream"].(type) {
	case bool:
		return t
	case string:
		return t == "true" || t == "1"
	default:
		return false
	}
}

func WriteError(policy, kind, message string) map[string]any {
	if message == "" {
		message = "Rejected by guardrail"
	}
	return map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    "content_filter",
			"param":   kind,
			"code":    "guardrail_intervened",
			"policy":  policy,
		},
	}
}
