package admin

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/javded-itres/mikrollm/internal/domain"
)

func (u *UI) agentsPage(w http.ResponseWriter, r *http.Request) {
	list, err := u.st.ListA2A()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	rows := make([]mcpRow, 0, len(list))
	for _, m := range list {
		rows = append(rows, mcpRow{
			ID: m.ID, Name: m.Name, URL: m.URL, Enabled: m.Enabled, HubShare: m.HubShare,
			TokenSet: strings.TrimSpace(m.Token) != "",
		})
	}
	cfg, _ := u.st.HubSettings()
	var remote []remoteMCP
	if u.hub != nil && cfg.Enabled {
		_, peers := u.hub.Peers()
		for _, p := range peers {
			for _, m := range p.Agents {
				remote = append(remote, remoteMCP{Node: p.ID, NodeName: p.Name, Name: m.Name, Online: p.Online})
			}
		}
	}
	u.render(w, r, "agents", map[string]any{
		"Title": "Агенты", "Nav": "agents",
		"Rows": rows, "Remote": remote,
		"CanShare": cfg.Enabled && cfg.ShareA2A,
		"HubOn":    cfg.Enabled,
		"Flash":    flashMsg(r.URL.Query().Get("ok")),
		"Error":    errMsg(r.URL.Query().Get("err")),
	})
}

func (u *UI) saveA2A(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)
	up := domain.A2AUpstream{
		ID: id, Name: r.FormValue("name"), URL: r.FormValue("url"), Token: r.FormValue("token"),
		Enabled: parseEnabled(r.FormValue("enabled")), HubShare: parseEnabled(r.FormValue("hub_share")),
	}
	cfg, err := u.st.HubSettings()
	canShare := err == nil && cfg.Enabled && cfg.ShareA2A
	if !canShare {
		up.HubShare = false
		if id > 0 {
			if old, err := u.st.GetA2A(id); err == nil {
				up.HubShare = old.HubShare
			}
		}
	}
	if _, err := u.st.SaveA2A(up); err != nil {
		http.Redirect(w, r, "/admin/agents?err="+url.QueryEscape(err.Error()), http.StatusFound)
		return
	}
	if u.hub != nil {
		u.hub.Kick()
	}
	http.Redirect(w, r, "/admin/agents?ok=agent_saved", http.StatusFound)
}

func (u *UI) deleteA2A(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if id > 0 {
		_ = u.st.DeleteA2A(id)
	}
	if u.hub != nil {
		u.hub.Kick()
	}
	http.Redirect(w, r, "/admin/agents?ok=agent_deleted", http.StatusFound)
}
