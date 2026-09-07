package admin

import (
	"net/http"
)

func (u *UI) chatPage(w http.ResponseWriter, r *http.Request) {
	aliases, catalog := u.chatModelLists()
	u.render(w, "chat", map[string]any{
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

func (u *UI) chatModelLists() (aliases, catalog []string) {
	seen := map[string]bool{}
	ms, _ := u.st.ListModels()
	for _, m := range ms {
		if !m.Enabled || m.Alias == "" || seen[m.Alias] {
			continue
		}
		aliases = append(aliases, m.Alias)
		seen[m.Alias] = true
	}
	bs, _ := u.st.ListBackends()
	for _, e := range u.health.Catalog(bs) {
		if e.Name == "" || seen[e.Name] {
			continue
		}
		catalog = append(catalog, e.Name)
		seen[e.Name] = true
	}
	return aliases, catalog
}

func (u *UI) modelNameList() []string {
	a, c := u.chatModelLists()
	return append(append([]string{}, a...), c...)
}
