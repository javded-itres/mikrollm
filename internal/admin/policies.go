package admin

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/javded-itres/mikrollm/internal/domain"
	"github.com/javded-itres/mikrollm/internal/guard"
)

func (u *UI) securityPage(w http.ResponseWriter, r *http.Request) {
	ps, _ := u.st.ListPolicies()
	ms, _ := u.st.ListModels()
	qs, _ := u.st.ListQueues()
	var edit domain.Policy
	if id, _ := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64); id > 0 {
		if p, err := u.st.GetPolicy(id); err == nil {
			edit = p
		}
	}
	u.render(w, r, "security", map[string]any{
		"Title": "Безопасность", "Nav": "security",
		"Policies": ps, "Edit": edit, "HasEdit": edit.ID > 0,
		"Models": ms, "Queues": qs,
		"Kinds": []struct{ ID, Label string }{
			{domain.GuardSystemPrompt, domain.GuardKindLabel(domain.GuardSystemPrompt)},
			{domain.GuardNSFW, domain.GuardKindLabel(domain.GuardNSFW)},
			{domain.GuardBlockWords, domain.GuardKindLabel(domain.GuardBlockWords)},
			{domain.GuardInjection, domain.GuardKindLabel(domain.GuardInjection)},
			{domain.GuardPII, domain.GuardKindLabel(domain.GuardPII)},
			{domain.GuardCategory, domain.GuardKindLabel(domain.GuardCategory)},
			{domain.GuardRegex, domain.GuardKindLabel(domain.GuardRegex)},
		},
		"PII": []struct{ ID, Label string }{
			{"email", "Email"}, {"phone", "Телефон"}, {"card", "Карта"}, {"ip", "IP"},
		},
		"Plugins":    guard.ListPlugins(),
		"EditWords":  strings.Join(edit.Config.Words, "\n"),
		"EditModels": joinTargets(edit.Targets, domain.GuardTargetModel),
		"Flash":      flashMsg(r.URL.Query().Get("ok")),
		"Error":      errMsg(r.URL.Query().Get("err")),
	})
}

func joinTargets(ts []domain.PolicyTarget, kind string) string {
	var out []string
	for _, t := range ts {
		if t.Kind == kind {
			out = append(out, t.Key)
		}
	}
	return strings.Join(out, "\n")
}

func hasTarget(ts []domain.PolicyTarget, kind, key string) bool {
	for _, t := range ts {
		if t.Kind == kind && t.Key == key {
			return true
		}
	}
	return false
}

func (u *UI) savePolicy(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)
	p := domain.Policy{
		ID: id, Name: strings.TrimSpace(r.FormValue("name")),
		Kind: r.FormValue("kind"), Action: r.FormValue("action"), Mode: r.FormValue("mode"),
		Enabled: r.FormValue("enabled") == "1",
		Config: domain.PolicyConfig{
			Prompt:     strings.TrimSpace(r.FormValue("prompt")),
			Words:      splitLines(r.FormValue("words")),
			Pattern:    strings.TrimSpace(r.FormValue("pattern")),
			PII:        r.Form["pii"],
			Categories: r.Form["category"],
			Plugins:    r.Form["plugin"],
		},
		Targets: parsePolicyTargets(r),
	}
	if _, err := u.st.SavePolicy(p); err != nil {
		http.Redirect(w, r, "/admin/security?err="+err.Error(), http.StatusFound)
		return
	}
	http.Redirect(w, r, "/admin/security?ok=saved", http.StatusFound)
}

func parsePolicyTargets(r *http.Request) []domain.PolicyTarget {
	var out []domain.PolicyTarget
	for _, a := range r.Form["alias"] {
		a = strings.TrimSpace(a)
		if a != "" {
			out = append(out, domain.PolicyTarget{Kind: domain.GuardTargetAlias, Key: a})
		}
	}
	for _, q := range r.Form["queue"] {
		q = strings.TrimSpace(q)
		if q != "" {
			out = append(out, domain.PolicyTarget{Kind: domain.GuardTargetQueue, Key: q})
		}
	}
	for _, m := range splitLines(r.FormValue("models")) {
		out = append(out, domain.PolicyTarget{Kind: domain.GuardTargetModel, Key: m})
	}
	return out
}

func splitLines(s string) []string {
	var out []string
	for _, p := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (u *UI) delPolicy(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	_ = u.st.DeletePolicy(id)
	http.Redirect(w, r, "/admin/security?ok=deleted", http.StatusFound)
}

func (u *UI) setPolicyEnabled(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	p, err := u.st.GetPolicy(id)
	if err != nil {
		http.Redirect(w, r, "/admin/security?err=no_backend", http.StatusFound)
		return
	}
	p.Enabled = parseEnabled(r.FormValue("enabled"))
	p.Targets = nil
	if _, err := u.st.SavePolicy(p); err != nil {
		http.Redirect(w, r, "/admin/security?err="+err.Error(), http.StatusFound)
		return
	}
	http.Redirect(w, r, "/admin/security?ok=saved", http.StatusFound)
}
