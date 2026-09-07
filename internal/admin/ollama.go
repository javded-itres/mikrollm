package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/javded-itres/mikrollm/internal/ports"
)

func (u *UI) backendByID(id int64) (ok bool, base, name string) {
	b, err := u.st.GetBackend(id)
	if err != nil || !b.Enabled {
		return false, "", ""
	}
	return true, b.BaseURL, b.Name
}

func (u *UI) ollamaJobs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jobs": u.jobs.List(), "running": u.jobs.Running()})
}

func (u *UI) ollamaPull(w http.ResponseWriter, r *http.Request) {
	u.startJob(w, r, "pull", "pulling", func(id, base, model string) error {
		wr := u.jobs.Writer(id)
		err := u.host.Pull(context.Background(), base, model, wr)
		_ = wr.Close()
		return err
	})
}

func (u *UI) ollamaDelete(w http.ResponseWriter, r *http.Request) {
	u.ollamaMutate(w, r, func(base, model string) error {
		return u.host.Delete(r.Context(), base, model)
	}, "deleted")
}

func (u *UI) ollamaUnload(w http.ResponseWriter, r *http.Request) {
	u.ollamaMutate(w, r, func(base, model string) error {
		return u.host.Unload(r.Context(), base, model)
	}, "unloaded")
}

func (u *UI) ollamaLoad(w http.ResponseWriter, r *http.Request) {
	u.startJob(w, r, "load", "loading", func(id, base, model string) error {
		u.jobs.SetMessage(id, "загрузка в RAM…")
		return u.host.Load(context.Background(), base, model)
	})
}

func (u *UI) startJob(w http.ResponseWriter, r *http.Request, kind, okFlash string, run func(id, base, model string) error) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	found, base, bname := u.backendByID(id)
	if !found {
		if wantsJSON(r) {
			writeJSONErr(w, http.StatusNotFound, "unknown backend")
			return
		}
		http.Redirect(w, r, nextPath(r)+"?err=unknown+backend", http.StatusFound)
		return
	}
	_ = r.ParseForm()
	model := strings.TrimSpace(r.FormValue("name"))
	if model == "" {
		if wantsJSON(r) {
			writeJSONErr(w, http.StatusBadRequest, "name required")
			return
		}
		http.Redirect(w, r, nextPath(r)+"?err=name+required", http.StatusFound)
		return
	}
	j, busy, err := u.jobs.Begin(kind, id, bname, model)
	if err != nil {
		msg := "busy"
		if busy != nil {
			msg = busy.Kind + " " + busy.Model
		}
		if wantsJSON(r) {
			code := http.StatusConflict
			if !errors.Is(err, ports.ErrBusy) {
				code = http.StatusBadGateway
			}
			writeJSONErr(w, code, msg)
			return
		}
		http.Redirect(w, r, nextPath(r)+"?err=busy", http.StatusFound)
		return
	}
	go func() {
		if err := run(j.ID, base, model); err != nil {
			u.jobs.Fail(j.ID, err.Error())
			return
		}
		u.jobs.Done(j.ID)
		u.health.CheckOnce()
	}()
	if wantsJSON(r) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "status": "started", "id": j.ID, "job": j})
		return
	}
	http.Redirect(w, r, nextPath(r)+"?ok="+okFlash, http.StatusFound)
}

func (u *UI) ollamaMutate(w http.ResponseWriter, r *http.Request, fn func(base, model string) error, ok string) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	found, base, _ := u.backendByID(id)
	if !found {
		if wantsJSON(r) {
			writeJSONErr(w, http.StatusNotFound, "unknown backend")
			return
		}
		http.Redirect(w, r, nextPath(r)+"?err=unknown+backend", http.StatusFound)
		return
	}
	_ = r.ParseForm()
	model := strings.TrimSpace(r.FormValue("name"))
	if model == "" {
		if wantsJSON(r) {
			writeJSONErr(w, http.StatusBadRequest, "name required")
			return
		}
		http.Redirect(w, r, nextPath(r)+"?err=name+required", http.StatusFound)
		return
	}
	if err := fn(base, model); err != nil {
		if wantsJSON(r) {
			writeJSONErr(w, http.StatusBadGateway, err.Error())
			return
		}
		http.Redirect(w, r, nextPath(r)+"?err="+url.QueryEscape(err.Error()), http.StatusFound)
		return
	}
	u.health.CheckOnce()
	if wantsJSON(r) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "status": ok})
		return
	}
	http.Redirect(w, r, nextPath(r)+"?ok="+ok, http.StatusFound)
}

func wantsJSON(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "application/json")
}

func nextPath(r *http.Request) string {
	next := strings.TrimSpace(r.FormValue("next"))
	if strings.HasPrefix(next, "/admin") && !strings.HasPrefix(next, "//") {
		return next
	}
	return "/admin/models"
}

func writeJSONErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": msg})
}

func humanSize(n int64) string {
	if n <= 0 {
		return ""
	}
	f := float64(n)
	u := []string{"B", "KB", "MB", "GB", "TB"}
	i := 0
	for f >= 1024 && i < len(u)-1 {
		f /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f %s", f, u[i])
}
