package admin

import (
	"net/http"

	"github.com/javded-itres/mikrollm/internal/domain"
)

func (u *UI) chatPage(w http.ResponseWriter, r *http.Request) {
	aliases, catalog := u.chatModelOpts()
	u.render(w, r, "chat", map[string]any{
		"Title": "Чат", "Nav": "chat", "Aliases": aliases, "Catalog": catalog,
	})
}

func (u *UI) chatPost(w http.ResponseWriter, r *http.Request) {
	if u.chat == nil {
		writeJSONErr(w, http.StatusServiceUnavailable, "chat gateway unavailable")
		return
	}
	u.chat.ServeChat(w, r)
}

type chatOpt struct {
	Value, Label string
}

func (u *UI) chatModelOpts() (aliases, catalog []chatOpt) {
	seen := map[string]bool{}
	bs, _ := u.st.ListBackends()
	byName := map[string]domain.CatalogEntry{}
	for _, e := range u.health.Catalog(bs) {
		byName[e.Name] = e
	}
	label := func(id string, e domain.CatalogEntry) string {
		s := id
		if e.Provider != "" {
			s = e.Provider + " · " + id
		}
		if p := domain.PriceLabel(e.Priced, e.PromptUSD, e.CompletionUSD); p != "" {
			s += " · " + p
		}
		return s
	}
	qs, _ := u.st.ListQueues()
	for _, q := range qs {
		if !q.Enabled {
			continue
		}
		for _, a := range q.AllAliases() {
			if a == "" || seen[a] {
				continue
			}
			aliases = append(aliases, chatOpt{Value: a, Label: "очередь · " + a})
			seen[a] = true
		}
	}
	ms, _ := u.st.ListModels()
	for _, m := range ms {
		if !m.Enabled || m.Alias == "" || seen[m.Alias] {
			continue
		}
		meta := byName[m.UpstreamName]
		if meta.Name == "" {
			meta = byName[m.Alias]
		}
		aliases = append(aliases, chatOpt{Value: m.Alias, Label: label(m.Alias, meta)})
		seen[m.Alias] = true
	}
	for _, e := range u.health.Catalog(bs) {
		if e.Name == "" || seen[e.Name] {
			continue
		}
		catalog = append(catalog, chatOpt{Value: e.Name, Label: label(e.Name, e)})
		seen[e.Name] = true
	}
	return aliases, catalog
}

func (u *UI) chatModelLists() (aliases, catalog []string) {
	a, c := u.chatModelOpts()
	for _, o := range a {
		aliases = append(aliases, o.Value)
	}
	for _, o := range c {
		catalog = append(catalog, o.Value)
	}
	return aliases, catalog
}

func (u *UI) modelNameList() []string {
	a, c := u.chatModelLists()
	return append(append([]string{}, a...), c...)
}
