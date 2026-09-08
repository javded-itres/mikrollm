package admin

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/javded-itres/mikrollm/internal/auth"
	"github.com/javded-itres/mikrollm/internal/web"
)

func staticHandler() http.Handler {
	sub, err := fs.Sub(web.FS, "static")
	if err != nil {
		panic(err)
	}
	return http.StripPrefix("/admin/static/", http.FileServer(http.FS(sub)))
}

func (u *UI) protect(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !u.keys.ValidSession(r) {
			http.Redirect(w, r, "/admin/login", http.StatusFound)
			return
		}
		if isUnsafe(r.Method) && !u.keys.ValidCSRF(r, csrfFromRequest(r)) {
			http.Error(w, "csrf", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func isUnsafe(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return false
	default:
		return true
	}
}

func csrfFromRequest(r *http.Request) string {
	if t := strings.TrimSpace(r.Header.Get("X-CSRF-Token")); t != "" {
		return t
	}
	if t := strings.TrimSpace(r.Header.Get("X-CSRF")); t != "" {
		return t
	}
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "application/x-www-form-urlencoded") || strings.HasPrefix(ct, "multipart/form-data") {
		_ = r.ParseForm()
		return r.FormValue("csrf")
	}
	return ""
}

func safeAdminPath(next, fallback string) string {
	next = strings.TrimSpace(next)
	if i := strings.IndexAny(next, "?#"); i >= 0 {
		next = next[:i]
	}
	if fallback == "" {
		fallback = "/admin"
	}
	if next == "" {
		return fallback
	}
	if !strings.HasPrefix(next, "/admin") || strings.HasPrefix(next, "//") || strings.Contains(next, "\\") || strings.Contains(next, "://") || strings.Contains(next, "@") {
		return fallback
	}
	return next
}

func nextPath(r *http.Request) string {
	return safeAdminPath(r.FormValue("next"), "/admin/models")
}

func clientIP(r *http.Request) string {
	return auth.ClientIP(r)
}
