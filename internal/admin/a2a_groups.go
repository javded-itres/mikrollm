package admin

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/javded-itres/mikrollm/internal/a2aproxy"
	"github.com/javded-itres/mikrollm/internal/auth"
	"github.com/javded-itres/mikrollm/internal/domain"
)

func (u *UI) saveA2AGroup(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	plain, hash, err := auth.GenerateInvite()
	if err != nil {
		http.Redirect(w, r, "/admin/agents?err=group+name", http.StatusFound)
		return
	}
	g := domain.A2AGroup{
		Name: r.FormValue("name"), Title: r.FormValue("title"), Description: r.FormValue("description"),
		InviteHash: hash,
	}
	if _, err := u.st.SaveA2AGroup(g); err != nil {
		http.Redirect(w, r, "/admin/agents?err="+url.QueryEscape(err.Error()), http.StatusFound)
		return
	}
	if u.keys != nil {
		u.keys.PutFlash(w, plain)
	}
	name, _ := domain.NormalizeMCPName(r.FormValue("name"))
	http.Redirect(w, r, "/admin/agents?ok=group_invite&group="+url.QueryEscape(name), http.StatusFound)
}

func (u *UI) rotateA2AInvite(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	g, err := u.st.GetA2AGroup(id)
	if err != nil {
		http.Redirect(w, r, "/admin/agents?err=group+name", http.StatusFound)
		return
	}
	plain, hash, err := auth.GenerateInvite()
	if err != nil || u.st.SetA2AInvite(id, hash) != nil {
		http.Redirect(w, r, "/admin/agents?err=group+name", http.StatusFound)
		return
	}
	if u.keys != nil {
		u.keys.PutFlash(w, plain)
	}
	http.Redirect(w, r, "/admin/agents?ok=group_invite&group="+url.QueryEscape(g.Name), http.StatusFound)
}

func (u *UI) deleteA2AGroup(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if id > 0 {
		_ = u.st.DeleteA2AGroup(id)
	}
	http.Redirect(w, r, "/admin/agents?ok=group_deleted", http.StatusFound)
}

func (u *UI) confirmA2AMessage(w http.ResponseWriter, r *http.Request) {
	u.decideA2AMessage(w, r, domain.A2APosted, "message_confirmed")
}

func (u *UI) rejectA2AMessage(w http.ResponseWriter, r *http.Request) {
	u.decideA2AMessage(w, r, domain.A2ARejected, "message_rejected")
}

func (u *UI) decideA2AMessage(w http.ResponseWriter, r *http.Request, status, ok string) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	msg, err := u.st.GetA2AMessage(id)
	if err != nil || msg.Status != domain.A2APending {
		http.Redirect(w, r, "/admin/agents?err=message", http.StatusFound)
		return
	}
	if err := u.st.SetA2AMessageStatus(id, status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Redirect(w, r, "/admin/agents?err=message", http.StatusFound)
			return
		}
		http.Redirect(w, r, "/admin/agents?err="+url.QueryEscape(err.Error()), http.StatusFound)
		return
	}
	if status == domain.A2APosted {
		grp, err := u.st.GetA2AGroup(msg.GroupID)
		if err == nil {
			msg.Group = grp.Name
			msg.Status = domain.A2APosted
			a2aproxy.ForwardPosted(context.Background(), u.messageTargets(msg), msg)
		}
	}
	http.Redirect(w, r, "/admin/agents?ok="+ok, http.StatusFound)
}

func (u *UI) messageTargets(msg domain.A2AMessage) []domain.A2AUpstream {
	if msg.ToAgent != "" {
		a, err := u.st.GetA2AByName(msg.ToAgent)
		if err != nil {
			return nil
		}
		return []domain.A2AUpstream{a}
	}
	members, err := u.st.ListGroupAgents(msg.GroupID)
	if err != nil {
		return nil
	}
	var out []domain.A2AUpstream
	for _, m := range members {
		if m.Name != msg.FromAgent {
			out = append(out, m)
		}
	}
	return out
}
