package admin

import (
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/javded-itres/mikrollm/internal/domain"
)

type agentRow struct {
	ID          int64
	Name        string
	URL         string
	DisplayName string
	Company     string
	Description string
	SkillsText  string
	Enabled     bool
	HubShare    bool
	TokenSet    bool
	Registered  bool
	GroupIDs    []int64
}

type groupView struct {
	ID          int64
	Name        string
	Title       string
	Description string
	Members     []string
	Messages    []msgView
}

type msgView struct {
	ID      int64
	From    string
	To      string
	Body    string
	Status  string
	Created string
}

func (u *UI) agentsPage(w http.ResponseWriter, r *http.Request) {
	list, err := u.st.ListA2A()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	groups, err := u.st.ListA2AGroups()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	members := map[int64][]string{}
	rows := make([]agentRow, 0, len(list))
	for _, m := range list {
		gids, err := u.st.AgentGroupIDs(m.ID)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		for _, id := range gids {
			members[id] = append(members[id], m.Name)
		}
		rows = append(rows, agentRow{
			ID: m.ID, Name: m.Name, URL: m.URL, DisplayName: m.DisplayName, Company: m.Company,
			Description: m.Description, SkillsText: domain.FormatSkillLines(m.Skills),
			Enabled: m.Enabled, HubShare: m.HubShare, TokenSet: strings.TrimSpace(m.Token) != "",
			Registered: strings.TrimSpace(m.AuthHash) != "", GroupIDs: gids,
		})
	}
	views := make([]groupView, 0, len(groups))
	for _, g := range groups {
		names := append([]string(nil), members[g.ID]...)
		sort.Strings(names)
		msgs, err := u.st.ListA2ATranscript(g.ID, 40)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		mv := make([]msgView, 0, len(msgs))
		for _, m := range msgs {
			mv = append(mv, msgView{ID: m.ID, From: m.FromAgent, To: m.ToAgent, Body: m.Body, Status: m.Status, Created: m.CreatedAt})
		}
		views = append(views, groupView{
			ID: g.ID, Name: g.Name, Title: g.Title, Description: g.Description, Members: names, Messages: mv,
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
	invite := ""
	if u.keys != nil {
		invite = u.keys.TakeFlash(w, r)
	}
	u.render(w, r, "agents", map[string]any{
		"Title": "Агенты", "Nav": "agents",
		"Rows": rows, "Groups": views, "Remote": remote,
		"CanShare": cfg.Enabled && cfg.ShareA2A,
		"HubOn":    cfg.Enabled,
		"Invite":   invite, "InviteGroup": r.URL.Query().Get("group"),
		"Flash": flashMsg(r.URL.Query().Get("ok")),
		"Error": errMsg(r.URL.Query().Get("err")),
	})
}

func (u *UI) saveA2A(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)
	up := domain.A2AUpstream{
		ID: id, Name: r.FormValue("name"), URL: r.FormValue("url"), Token: r.FormValue("token"),
		Enabled: parseEnabled(r.FormValue("enabled")), HubShare: parseEnabled(r.FormValue("hub_share")),
		DisplayName: r.FormValue("display_name"), Company: r.FormValue("company"),
		Description: r.FormValue("description"), Skills: domain.ParseSkillLines(r.FormValue("skills")),
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
	id, err = u.st.SaveA2A(up)
	if err != nil {
		http.Redirect(w, r, "/admin/agents?err="+url.QueryEscape(err.Error()), http.StatusFound)
		return
	}
	var gids []int64
	for _, raw := range r.Form["groups"] {
		n, _ := strconv.ParseInt(raw, 10, 64)
		if n > 0 {
			gids = append(gids, n)
		}
	}
	if err := u.st.SetAgentGroups(id, gids); err != nil {
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
