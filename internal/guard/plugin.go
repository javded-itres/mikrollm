package guard

import (
	"sort"
	"strings"
	"sync"
	"unicode"
)

// Plugin is a LiteLLM-style content_filter category: keyword list you enable on a policy.
// Built-in plugins register in init; extra words can still be added on the policy.
type Plugin struct {
	ID    string
	Label string
	Group string
	Words []string
}

var (
	pluginMu sync.RWMutex
	plugins  = map[string]*Plugin{}
)

func Register(p Plugin) {
	p.ID = strings.TrimSpace(strings.ToLower(p.ID))
	if p.ID == "" || len(p.Words) == 0 {
		return
	}
	pluginMu.Lock()
	plugins[p.ID] = &p
	pluginMu.Unlock()
}

func Lookup(id string) *Plugin {
	pluginMu.RLock()
	defer pluginMu.RUnlock()
	return plugins[strings.ToLower(strings.TrimSpace(id))]
}

func ListPlugins() []Plugin {
	pluginMu.RLock()
	defer pluginMu.RUnlock()
	out := make([]Plugin, 0, len(plugins))
	for _, p := range plugins {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			return out[i].Group < out[j].Group
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func MatchPlugins(text string, ids []string) (string, bool) {
	low := strings.ToLower(text)
	for _, id := range ids {
		id = strings.ToLower(strings.TrimSpace(id))
		if id == "sexual" {
			id = "csam"
		}
		p := Lookup(id)
		if p == nil {
			continue
		}
		for _, w := range p.Words {
			if keywordHit(low, w) {
				return p.ID, true
			}
		}
	}
	return "", false
}

func keywordHit(low, word string) bool {
	word = strings.ToLower(strings.TrimSpace(word))
	if word == "" {
		return false
	}
	if strings.ContainsRune(word, ' ') {
		return strings.Contains(low, word)
	}
	return hasWord(low, word)
}

// hasWord matches a token at unicode letter/digit boundaries (LiteLLM word-boundary for single words).
func hasWord(low, word string) bool {
	rs := []rune(low)
	ws := []rune(word)
	if len(ws) == 0 || len(rs) < len(ws) {
		return false
	}
	for i := 0; i <= len(rs)-len(ws); i++ {
		ok := true
		for j := 0; j < len(ws); j++ {
			if rs[i+j] != ws[j] {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		if i > 0 && isWordChar(rs[i-1]) {
			continue
		}
		if i+len(ws) < len(rs) && isWordChar(rs[i+len(ws)]) {
			continue
		}
		return true
	}
	return false
}

func isWordChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

func selectedPlugins(kind string, plugins, categories []string) []string {
	ids := append([]string{}, plugins...)
	if len(ids) == 0 {
		ids = append(ids, categories...)
	}
	if kind == "nsfw" && len(ids) == 0 {
		return []string{"nsfw", "adult"}
	}
	return ids
}
