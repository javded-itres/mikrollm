package admin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/javded-itres/mikrollm/internal/domain"
)

func (u *UI) queueSnapshot() []domain.QueueView {
	if u.queues == nil {
		return nil
	}
	return u.queues.Snapshot()
}

func (u *UI) queuesPage(w http.ResponseWriter, r *http.Request) {
	ms, _ := u.st.ListModels()
	u.render(w, r, "queues", map[string]any{
		"Title": "Очереди", "Nav": "queues", "Queues": u.queueSnapshot(), "Models": ms,
		"Flash": flashMsg(r.URL.Query().Get("ok")), "Error": errMsg(r.URL.Query().Get("err")),
	})
}

func (u *UI) queuesLive(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"queues": u.queueSnapshot()})
}

func (u *UI) saveQueue(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)
	ov, _ := strconv.Atoi(r.FormValue("overflow_after"))
	waitMS, _ := strconv.Atoi(r.FormValue("max_wait_ms"))
	q := domain.Queue{
		ID: id, Name: strings.TrimSpace(r.FormValue("name")),
		Alias: strings.TrimSpace(r.FormValue("alias")), Enabled: true,
		OverflowAfter: ov, OverflowAlias: strings.TrimSpace(r.FormValue("overflow_alias")),
		MaxWaitMS: waitMS,
	}
	if id > 0 {
		old, err := u.st.GetQueue(id)
		if err != nil {
			http.Redirect(w, r, "/admin/queues?err="+err.Error(), http.StatusFound)
			return
		}
		q.Steps = old.Steps
		q.ExtraAliases = old.ExtraAliases
	}
	if _, err := u.st.SaveQueue(q); err != nil {
		http.Redirect(w, r, "/admin/queues?err="+err.Error(), http.StatusFound)
		return
	}
	http.Redirect(w, r, "/admin/queues?ok=queue_saved", http.StatusFound)
}

func (u *UI) delQueue(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	_ = u.st.DeleteQueue(id)
	http.Redirect(w, r, "/admin/queues?ok=queue_deleted", http.StatusFound)
}

func (u *UI) addQueueStep(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	_ = r.ParseForm()
	q, err := u.st.GetQueue(id)
	if err != nil {
		http.Redirect(w, r, "/admin/queues?err="+err.Error(), http.StatusFound)
		return
	}
	alias := strings.TrimSpace(r.FormValue("model_alias"))
	capn, _ := strconv.Atoi(r.FormValue("max_concurrent"))
	if alias == "" {
		http.Redirect(w, r, "/admin/queues?err=select_models", http.StatusFound)
		return
	}
	if capn <= 0 {
		capn = 1
	}
	q.Steps = append(q.Steps, domain.QueueStep{ModelAlias: alias, MaxConcurrent: capn})
	if _, err := u.st.SaveQueue(q); err != nil {
		http.Redirect(w, r, "/admin/queues?err="+err.Error(), http.StatusFound)
		return
	}
	http.Redirect(w, r, "/admin/queues?ok=queue_saved", http.StatusFound)
}

func (u *UI) upQueueStep(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	pos, _ := strconv.Atoi(r.PathValue("pos"))
	q, err := u.st.GetQueue(id)
	if err != nil {
		http.Redirect(w, r, "/admin/queues?err="+err.Error(), http.StatusFound)
		return
	}
	if pos <= 0 || pos >= len(q.Steps) {
		http.Redirect(w, r, "/admin/queues", http.StatusFound)
		return
	}
	q.Steps[pos-1], q.Steps[pos] = q.Steps[pos], q.Steps[pos-1]
	if _, err := u.st.SaveQueue(q); err != nil {
		http.Redirect(w, r, "/admin/queues?err="+err.Error(), http.StatusFound)
		return
	}
	http.Redirect(w, r, "/admin/queues?ok=queue_saved", http.StatusFound)
}

func (u *UI) delQueueStep(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	pos, _ := strconv.Atoi(r.PathValue("pos"))
	q, err := u.st.GetQueue(id)
	if err != nil {
		http.Redirect(w, r, "/admin/queues?err="+err.Error(), http.StatusFound)
		return
	}
	if pos < 0 || pos >= len(q.Steps) {
		http.Redirect(w, r, "/admin/queues", http.StatusFound)
		return
	}
	q.Steps = append(q.Steps[:pos], q.Steps[pos+1:]...)
	if _, err := u.st.SaveQueue(q); err != nil {
		http.Redirect(w, r, "/admin/queues?err="+err.Error(), http.StatusFound)
		return
	}
	http.Redirect(w, r, "/admin/queues?ok=queue_saved", http.StatusFound)
}

func (u *UI) addQueueAlias(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	_ = r.ParseForm()
	if err := u.st.AddQueueAlias(id, r.FormValue("alias")); err != nil {
		http.Redirect(w, r, "/admin/queues?err="+err.Error(), http.StatusFound)
		return
	}
	http.Redirect(w, r, "/admin/queues?ok=queue_saved", http.StatusFound)
}

func (u *UI) delQueueAlias(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	_ = r.ParseForm()
	_ = u.st.DeleteQueueAlias(id, r.FormValue("alias"))
	http.Redirect(w, r, "/admin/queues?ok=queue_saved", http.StatusFound)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
