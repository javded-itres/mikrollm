package admin

import (
	"net/http"
	"strings"

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

func (u *UI) imagesPost(w http.ResponseWriter, r *http.Request) {
	if u.chat == nil {
		writeJSONErr(w, http.StatusServiceUnavailable, "chat gateway unavailable")
		return
	}
	u.chat.ServeImages(w, r)
}

func (u *UI) videosPost(w http.ResponseWriter, r *http.Request) {
	if u.chat == nil {
		writeJSONErr(w, http.StatusServiceUnavailable, "chat gateway unavailable")
		return
	}
	u.chat.ServeVideos(w, r)
}

func (u *UI) videoStatus(w http.ResponseWriter, r *http.Request) {
	if u.chat == nil {
		writeJSONErr(w, http.StatusServiceUnavailable, "chat gateway unavailable")
		return
	}
	u.chat.ServeVideoStatus(w, r)
}

func (u *UI) videoContent(w http.ResponseWriter, r *http.Request) {
	if u.chat == nil {
		writeJSONErr(w, http.StatusServiceUnavailable, "chat gateway unavailable")
		return
	}
	u.chat.ServeVideoContent(w, r)
}

func (u *UI) modelParams(w http.ResponseWriter, r *http.Request) {
	if u.chat == nil {
		writeJSONErr(w, http.StatusServiceUnavailable, "chat gateway unavailable")
		return
	}
	u.chat.ServeModelParams(w, r)
}

type chatOpt struct {
	Value, Label, Media string
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
		if p := domain.FormatCatalogPrice(e.Priced, e.PromptUSD, e.CompletionUSD, e.ImageUSD, e.ImageTokUSD, e.VideoSecUSD); p != "" {
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
			aliases = append(aliases, chatOpt{Value: a, Label: "очередь · " + a, Media: ""})
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
		media := domain.MergeMedia(m.Media, meta.Media)
		if len(media) == 0 {
			media = domain.InferMedia(m.Alias, nil)
			if m.UpstreamName != "" {
				media = domain.MergeMedia(media, domain.InferMedia(m.UpstreamName, nil))
			}
		}
		ml := domain.MediaLabel(media)
		lb := label(m.Alias, meta)
		if m.HubNodeName != "" {
			lb = m.HubNodeName + " · " + lb
		}
		if ml != "" {
			lb += " · " + ml
		}
		aliases = append(aliases, chatOpt{Value: m.Alias, Label: lb, Media: strings.Join(media, ",")})
		seen[m.Alias] = true
	}
	for _, e := range u.health.Catalog(bs) {
		if e.Name == "" {
			continue
		}
		val := e.Name
		if e.HubNodeID != "" {
			val = "hub|" + e.HubNodeID + "|" + e.Name
			if seen[val] || seen[e.Name] {
				continue
			}
		} else if seen[e.Name] {
			continue
		}
		ml := domain.MediaLabel(e.Media)
		lb := label(e.Name, e)
		if e.HubNodeName != "" {
			lb = e.HubNodeName + " · " + lb
		}
		if ml != "" {
			lb += " · " + ml
		}
		catalog = append(catalog, chatOpt{Value: val, Label: lb, Media: strings.Join(e.Media, ",")})
		seen[val] = true
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
