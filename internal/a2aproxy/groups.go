package a2aproxy

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/javded-itres/mikrollm/internal/auth"
	"github.com/javded-itres/mikrollm/internal/domain"
)

// GroupStore is the local agent directory. Groups do not leave this gateway.
type GroupStore interface {
	GetA2AByName(name string) (domain.A2AUpstream, error)
	GetA2AByAuthHash(hash string) (domain.A2AUpstream, error)
	SaveA2A(domain.A2AUpstream) (int64, error)
	GetA2AGroup(id int64) (domain.A2AGroup, error)
	GetA2AGroupByInvite(hash string) (domain.A2AGroup, error)
	AgentGroups(agentID int64) ([]domain.A2AGroup, error)
	AddAgentToGroup(agentID, groupID int64) error
	ListGroupAgents(groupID int64) ([]domain.A2AUpstream, error)
	InsertA2AMessage(domain.A2AMessage) (domain.A2AMessage, error)
	ListA2AMessages(groupID, after int64, limit int) ([]domain.A2AMessage, error)
}

type regReq struct {
	Invite      string            `json:"invite"`
	Name        string            `json:"name"`
	Token       string            `json:"token"`
	DisplayName string            `json:"display_name"`
	Company     string            `json:"company"`
	Description string            `json:"description"`
	URL         *string           `json:"url"`
	Skills      []domain.A2ASkill `json:"skills"`
}

type msgReq struct {
	Group   string `json:"group"`
	To      string `json:"to"`
	Text    string `json:"text"`
	Confirm bool   `json:"confirm"`
}

// MountGroups registers peer discovery and the group transcript.
// Callers use the agt- bearer from POST /a2a/register, not the MCP secret.
func MountGroups(mux *http.ServeMux, st GroupStore) {
	g := &groups{st: st}
	mux.HandleFunc("POST /a2a/register", g.register)
	mux.HandleFunc("GET /a2a/directory", g.directory)
	mux.HandleFunc("GET /a2a/messages", g.listMessages)
	mux.HandleFunc("POST /a2a/messages", g.postMessage)
}

type groups struct {
	st GroupStore
}

func (g *groups) register(w http.ResponseWriter, r *http.Request) {
	var req regReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeAPIErr(w, http.StatusBadRequest, "bad body")
		return
	}
	if strings.TrimSpace(req.Invite) == "" {
		writeAPIErr(w, http.StatusUnauthorized, "invite")
		return
	}
	grp, err := g.st.GetA2AGroupByInvite(auth.HashKey(strings.TrimSpace(req.Invite)))
	if errors.Is(err, sql.ErrNoRows) {
		writeAPIErr(w, http.StatusUnauthorized, "invite")
		return
	}
	if err != nil {
		writeAPIErr(w, http.StatusInternalServerError, "store")
		return
	}
	name, err := domain.NormalizeMCPName(req.Name)
	if err != nil {
		writeAPIErr(w, http.StatusBadRequest, "agent name")
		return
	}
	up, err := g.st.GetA2AByName(name)
	created := false
	var plain string
	if errors.Is(err, sql.ErrNoRows) {
		plain, _, err = auth.GenerateAgentToken()
		if err != nil {
			writeAPIErr(w, http.StatusInternalServerError, "store")
			return
		}
		up = domain.A2AUpstream{Name: name, Enabled: true, AuthHash: auth.HashKey(plain)}
		created = true
	} else if err != nil {
		writeAPIErr(w, http.StatusInternalServerError, "store")
		return
	} else if !up.Enabled || !hashEq(up.AuthHash, req.Token) {
		if !up.Enabled {
			writeAPIErr(w, http.StatusForbidden, "disabled")
			return
		}
		writeAPIErr(w, http.StatusConflict, "agent name taken")
		return
	}
	up.DisplayName = req.DisplayName
	up.Company = req.Company
	up.Description = req.Description
	if req.Skills != nil {
		up.Skills = req.Skills
	}
	if req.URL != nil {
		up.URL = strings.TrimSpace(*req.URL)
	}
	up.Clean()
	id, err := g.st.SaveA2A(up)
	if err != nil {
		code := http.StatusBadRequest
		if err.Error() == "agent name taken" {
			code = http.StatusConflict
		}
		writeAPIErr(w, code, err.Error())
		return
	}
	if err := g.st.AddAgentToGroup(id, grp.ID); err != nil {
		writeAPIErr(w, http.StatusInternalServerError, "store")
		return
	}
	names := g.groupNames(id)
	resp := map[string]any{"name": name, "group": grp.Name, "groups": names}
	if created {
		resp["token"] = plain
	}
	code := http.StatusOK
	if created {
		code = http.StatusCreated
	}
	writeJSON(w, code, resp)
}

func (g *groups) directory(w http.ResponseWriter, r *http.Request) {
	self, ok := g.caller(w, r)
	if !ok {
		return
	}
	mine, err := g.st.AgentGroups(self.ID)
	if err != nil {
		writeAPIErr(w, http.StatusInternalServerError, "store")
		return
	}
	mineSet := map[string]bool{}
	var selfGroups []string
	for _, grp := range mine {
		mineSet[grp.Name] = true
		selfGroups = append(selfGroups, grp.Name)
	}
	seen := map[int64]bool{}
	var agents []any
	for _, grp := range mine {
		members, err := g.st.ListGroupAgents(grp.ID)
		if err != nil {
			writeAPIErr(w, http.StatusInternalServerError, "store")
			return
		}
		for _, m := range members {
			if m.ID == self.ID || !m.Enabled || seen[m.ID] {
				continue
			}
			theirs, err := g.st.AgentGroups(m.ID)
			if err != nil {
				writeAPIErr(w, http.StatusInternalServerError, "store")
				return
			}
			var inter []string
			for _, tg := range theirs {
				if mineSet[tg.Name] {
					inter = append(inter, tg.Name)
				}
			}
			if len(inter) == 0 {
				continue
			}
			seen[m.ID] = true
			agents = append(agents, publicAgent(m, inter))
		}
	}
	if agents == nil {
		agents = []any{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"self":   publicAgent(self, selfGroups),
		"agents": agents,
	})
}

func (g *groups) listMessages(w http.ResponseWriter, r *http.Request) {
	self, ok := g.caller(w, r)
	if !ok {
		return
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	groups, ok := g.memberGroups(w, self.ID, r.URL.Query().Get("group"))
	if !ok {
		return
	}
	var out []map[string]any
	for _, grp := range groups {
		list, err := g.st.ListA2AMessages(grp.ID, after, 100)
		if err != nil {
			writeAPIErr(w, http.StatusInternalServerError, "store")
			return
		}
		for _, m := range list {
			if m.Status == domain.A2ARejected {
				continue
			}
			if m.Status == domain.A2APending && m.FromAgent != self.Name {
				continue
			}
			m.Group = grp.Name
			out = append(out, publicMessage(m))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i]["id"].(int64) < out[j]["id"].(int64)
	})
	if len(out) > 100 {
		out = out[:100]
	}
	if out == nil {
		out = []map[string]any{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": out})
}

func (g *groups) postMessage(w http.ResponseWriter, r *http.Request) {
	self, ok := g.caller(w, r)
	if !ok {
		return
	}
	var req msgReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeAPIErr(w, http.StatusBadRequest, "bad body")
		return
	}
	groups, ok := g.memberGroups(w, self.ID, req.Group)
	if !ok {
		return
	}
	grp := groups[0]
	if strings.TrimSpace(req.Group) == "" {
		writeAPIErr(w, http.StatusBadRequest, "group name")
		return
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		writeAPIErr(w, http.StatusBadRequest, "message")
		return
	}
	var target domain.A2AUpstream
	to := strings.TrimSpace(req.To)
	if to != "" {
		name, err := domain.NormalizeMCPName(to)
		if err != nil {
			writeAPIErr(w, http.StatusBadRequest, "agent name")
			return
		}
		target, err = g.st.GetA2AByName(name)
		if err != nil || !target.Enabled || !g.inGroup(target.ID, grp.ID) {
			writeAPIErr(w, http.StatusForbidden, "not in group")
			return
		}
		to = target.Name
	}
	status := domain.A2APosted
	if req.Confirm {
		status = domain.A2APending
	}
	msg, err := g.st.InsertA2AMessage(domain.A2AMessage{
		GroupID: grp.ID, FromAgent: self.Name, ToAgent: to, Body: text, Status: status,
	})
	if err != nil {
		writeAPIErr(w, http.StatusBadRequest, "message")
		return
	}
	msg.Group = grp.Name
	if status == domain.A2APosted {
		ForwardPosted(context.Background(), g.destinations(grp.ID, self.Name, target, to), msg)
	}
	writeJSON(w, http.StatusCreated, map[string]any{"message": publicMessage(msg)})
}

func (g *groups) destinations(groupID int64, from string, target domain.A2AUpstream, to string) []domain.A2AUpstream {
	if to != "" {
		return []domain.A2AUpstream{target}
	}
	members, err := g.st.ListGroupAgents(groupID)
	if err != nil {
		return nil
	}
	var out []domain.A2AUpstream
	for _, m := range members {
		if m.Name != from {
			out = append(out, m)
		}
	}
	return out
}

func (g *groups) inGroup(agentID, groupID int64) bool {
	groups, err := g.st.AgentGroups(agentID)
	if err != nil {
		return false
	}
	for _, grp := range groups {
		if grp.ID == groupID {
			return true
		}
	}
	return false
}

func (g *groups) memberGroups(w http.ResponseWriter, agentID int64, name string) ([]domain.A2AGroup, bool) {
	mine, err := g.st.AgentGroups(agentID)
	if err != nil {
		writeAPIErr(w, http.StatusInternalServerError, "store")
		return nil, false
	}
	name = strings.TrimSpace(name)
	if name == "" {
		if len(mine) == 0 {
			writeAPIErr(w, http.StatusForbidden, "not in group")
			return nil, false
		}
		return mine, true
	}
	norm, err := domain.NormalizeMCPName(name)
	if err != nil {
		writeAPIErr(w, http.StatusBadRequest, "group name")
		return nil, false
	}
	for _, grp := range mine {
		if grp.Name == norm {
			return []domain.A2AGroup{grp}, true
		}
	}
	writeAPIErr(w, http.StatusForbidden, "not in group")
	return nil, false
}

func (g *groups) groupNames(agentID int64) []string {
	groups, err := g.st.AgentGroups(agentID)
	if err != nil || len(groups) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(groups))
	for _, grp := range groups {
		out = append(out, grp.Name)
	}
	return out
}

func (g *groups) caller(w http.ResponseWriter, r *http.Request) (domain.A2AUpstream, bool) {
	tok := auth.Bearer(r)
	if !strings.HasPrefix(tok, "agt-") || len(tok) > 200 {
		writeAPIErr(w, http.StatusUnauthorized, "unauthorized")
		return domain.A2AUpstream{}, false
	}
	up, err := g.st.GetA2AByAuthHash(auth.HashKey(tok))
	if err != nil || !up.Enabled {
		writeAPIErr(w, http.StatusUnauthorized, "unauthorized")
		return domain.A2AUpstream{}, false
	}
	return up, true
}

func hashEq(hash, plain string) bool {
	plain = strings.TrimSpace(plain)
	if hash == "" || plain == "" {
		return false
	}
	got := auth.HashKey(plain)
	if len(got) != len(hash) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(hash)) == 1
}

func publicAgent(u domain.A2AUpstream, groups []string) map[string]any {
	if groups == nil {
		groups = []string{}
	}
	skills := u.Skills
	if skills == nil {
		skills = []domain.A2ASkill{}
	}
	return map[string]any{
		"name":         u.Name,
		"display_name": u.DisplayName,
		"company":      u.Company,
		"description":  u.Description,
		"skills":       skills,
		"groups":       groups,
		"push":         strings.TrimSpace(u.URL) != "",
	}
}

func publicMessage(m domain.A2AMessage) map[string]any {
	return map[string]any{
		"id":         m.ID,
		"group":      m.Group,
		"from":       m.FromAgent,
		"to":         m.ToAgent,
		"text":       m.Body,
		"status":     m.Status,
		"created_at": m.CreatedAt,
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func writeAPIErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
