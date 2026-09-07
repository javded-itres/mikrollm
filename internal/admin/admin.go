package admin

import (
	"html/template"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/javded-itres/mikrollm/internal/auth"
	"github.com/javded-itres/mikrollm/internal/domain"
	"github.com/javded-itres/mikrollm/internal/ports"
	"github.com/javded-itres/mikrollm/internal/web"
)

type Deps struct {
	Store  ports.Store
	Health ports.Health
	Auth   ports.Auth
	Host   ports.Host
	Jobs   ports.Jobs
	Chat   ports.ChatGateway
}

type UI struct {
	st     ports.Store
	health ports.Health
	keys   ports.Auth
	host   ports.Host
	jobs   ports.Jobs
	chat   ports.ChatGateway
	pages  map[string]*template.Template
	login  *template.Template
}

func New(d Deps) *UI {
	fm := template.FuncMap{
		"hsize": humanSize,
		"hctx":  domain.FormatContext,
		"join":  strings.Join,
	}
	must := func(files ...string) *template.Template {
		return template.Must(template.New("layout.html").Funcs(fm).ParseFS(web.FS, files...))
	}
	return &UI{
		st: d.Store, health: d.Health, keys: d.Auth, host: d.Host, jobs: d.Jobs, chat: d.Chat,
		login: template.Must(template.New("login.html").Funcs(fm).ParseFS(web.FS, "templates/login.html")),
		pages: map[string]*template.Template{
			"dash":   must("templates/layout.html", "templates/dash.html"),
			"models": must("templates/layout.html", "templates/models.html"),
			"keys":   must("templates/layout.html", "templates/keys.html"),
			"chat":   must("templates/layout.html", "templates/chat.html"),
			"logs":   must("templates/layout.html", "templates/logs.html"),
		},
	}
}

func (u *UI) Mount(mux *http.ServeMux) {
	static := http.FileServer(http.FS(web.FS))
	mux.Handle("GET /admin/static/", http.StripPrefix("/admin/", static))
	mux.HandleFunc("GET /admin/login", u.loginPage)
	mux.HandleFunc("POST /admin/login", u.loginPost)
	mux.HandleFunc("POST /admin/logout", u.logout)
	mux.HandleFunc("GET /admin", u.protect(u.dash))
	mux.HandleFunc("POST /admin/refresh", u.protect(u.refresh))
	mux.HandleFunc("POST /admin/backends", u.protect(u.addBackend))
	mux.HandleFunc("POST /admin/backends/{id}/delete", u.protect(u.delBackend))
	mux.HandleFunc("POST /admin/password", u.protect(u.password))
	mux.HandleFunc("GET /admin/models", u.protect(u.models))
	mux.HandleFunc("POST /admin/models", u.protect(u.saveModel))
	mux.HandleFunc("POST /admin/models/connect", u.protect(u.connectModels))
	mux.HandleFunc("POST /admin/models/{id}/delete", u.protect(u.delModel))
	mux.HandleFunc("POST /admin/models/{id}/context", u.protect(u.setModelContext))
	mux.HandleFunc("POST /admin/models/{id}/fallback", u.protect(u.setModelFallback))
	mux.HandleFunc("GET /admin/ollama/jobs", u.protect(u.ollamaJobs))
	mux.HandleFunc("POST /admin/ollama/{id}/pull", u.protect(u.ollamaPull))
	mux.HandleFunc("POST /admin/ollama/{id}/delete", u.protect(u.ollamaDelete))
	mux.HandleFunc("POST /admin/ollama/{id}/unload", u.protect(u.ollamaUnload))
	mux.HandleFunc("POST /admin/ollama/{id}/load", u.protect(u.ollamaLoad))
	mux.HandleFunc("GET /admin/keys", u.protect(u.keysPage))
	mux.HandleFunc("POST /admin/keys", u.protect(u.createKey))
	mux.HandleFunc("POST /admin/keys/{id}", u.protect(u.updateKey))
	mux.HandleFunc("POST /admin/keys/{id}/delete", u.protect(u.delKey))
	mux.HandleFunc("GET /admin/chat", u.protect(u.chatPage))
	mux.HandleFunc("POST /admin/chat", u.protect(u.chatPost))
	mux.HandleFunc("GET /admin/logs", u.protect(u.logs))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/admin", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	})
}

func (u *UI) protect(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !u.keys.ValidSession(r) {
			http.Redirect(w, r, "/admin/login", http.StatusFound)
			return
		}
		next(w, r)
	}
}

func (u *UI) loginPage(w http.ResponseWriter, r *http.Request) {
	if u.keys.ValidSession(r) {
		http.Redirect(w, r, "/admin", http.StatusFound)
		return
	}
	_ = u.login.Execute(w, map[string]any{"Error": r.URL.Query().Get("err")})
}

func (u *UI) loginPost(w http.ResponseWriter, r *http.Request) {
	ip := r.RemoteAddr
	if u.keys.LoginBlocked(ip) {
		http.Redirect(w, r, "/admin/login?err=too+many+attempts", http.StatusFound)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	_ = r.ParseForm()
	ok := u.keys.CheckPassword(r.FormValue("password"))
	u.keys.RecordLogin(ip, ok)
	if !ok {
		http.Redirect(w, r, "/admin/login?err=неверный+пароль", http.StatusFound)
		return
	}
	if err := u.keys.IssueCookie(w, r); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusFound)
}

func (u *UI) logout(w http.ResponseWriter, r *http.Request) {
	u.keys.ClearCookie(w)
	http.Redirect(w, r, "/admin/login", http.StatusFound)
}

type backendVM struct {
	domain.Backend
	Healthy  bool
	Latency  string
	Err      string
	ErrShort string
	Models   []string
	Running  []string
	NModels  int
	NRunning int
	Status   string
}

func (u *UI) dash(w http.ResponseWriter, r *http.Request) {
	bs, _ := u.st.ListBackends()
	var vms []backendVM
	up := 0
	for _, b := range bs {
		st := u.health.Get(b.ID)
		lat := "—"
		if !st.Checked.IsZero() {
			lat = st.Latency.Truncate(time.Millisecond).String()
		}
		status := "Нет связи"
		if st.Healthy {
			up++
			status = "Онлайн"
		}
		errShort := st.Error
		if len(errShort) > 88 {
			errShort = errShort[:85] + "…"
		}
		vms = append(vms, backendVM{
			Backend: b, Healthy: st.Healthy, Latency: lat, Err: st.Error, ErrShort: errShort,
			Models: st.Models, Running: st.Running, NModels: len(st.Models),
			NRunning: len(st.Running), Status: status,
		})
	}
	aliases, _ := u.st.ListModels()
	u.render(w, "dash", map[string]any{
		"Title": "Статус", "Nav": "dash", "Backends": vms,
		"Up": up, "Total": len(vms), "AliasCount": len(aliases),
		"Flash": flashMsg(r.URL.Query().Get("ok")), "Error": errMsg(r.URL.Query().Get("err")),
	})
}

func (u *UI) refresh(w http.ResponseWriter, r *http.Request) {
	u.health.CheckOnce()
	next := r.FormValue("next")
	if next == "" {
		next = "/admin"
	}
	http.Redirect(w, r, next+"?ok=refreshed", http.StatusFound)
}

func (u *UI) addBackend(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	name := strings.TrimSpace(r.FormValue("name"))
	kind := strings.TrimSpace(r.FormValue("kind"))
	token := strings.TrimSpace(r.FormValue("token"))
	url := domain.CanonicalBaseURL(kind, r.FormValue("base_url"))
	if name == "" || url == "" {
		http.Redirect(w, r, "/admin?err=name+and+url+required", http.StatusFound)
		return
	}
	if domain.RequiresToken(kind) && token == "" {
		http.Redirect(w, r, "/admin?err=token+required", http.StatusFound)
		return
	}
	if _, err := u.st.UpsertBackend(name, url, true, 1, kind, token); err != nil {
		http.Redirect(w, r, "/admin?err="+err.Error(), http.StatusFound)
		return
	}
	go u.health.CheckOnce()
	http.Redirect(w, r, "/admin?ok=backend_saved", http.StatusFound)
}

func (u *UI) delBackend(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	_ = u.st.DeleteBackend(id)
	http.Redirect(w, r, "/admin?ok=backend_deleted", http.StatusFound)
}

func (u *UI) password(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	pw := r.FormValue("password")
	if len(pw) < 6 {
		http.Redirect(w, r, "/admin?err=min+6+chars", http.StatusFound)
		return
	}
	if err := u.st.SetAdminPassword(pw); err != nil {
		http.Redirect(w, r, "/admin?err="+err.Error(), http.StatusFound)
		return
	}
	http.Redirect(w, r, "/admin?ok=password_updated", http.StatusFound)
}

type modelVM struct {
	domain.Model
	BackendNames string
	CtxLabel     string
	Detected     int
	Provider     string
	PriceLabel   string
	FallbackOK   bool
}

type catalogHost struct {
	ID        int64
	Name      string
	Short     string
	Loaded    bool
	Kind      string
	Label     string
	CanLoad   bool
	CanDelete bool
	Cloud     bool
	Hint      string
}

type catalogVM struct {
	domain.CatalogEntry
	Connected    bool
	BackendLabel string
	SizeLabel    string
	LoadedLabel  string
	CtxLabel     string
	PriceLabel   string
	PriceBand    string
	Hosts        []catalogHost
}

func (u *UI) models(w http.ResponseWriter, r *http.Request) {
	ms, _ := u.st.ListModels()
	bs, _ := u.st.ListBackends()
	bmap := map[int64]domain.Backend{}
	var pullers []domain.Backend
	hasVLLM := false
	hasCloud := false
	for _, b := range bs {
		bmap[b.ID] = b
		if b.CanPull() {
			pullers = append(pullers, b)
		}
		if b.KindNorm() == domain.KindVLLM {
			hasVLLM = true
		}
		if b.Cloud() {
			hasCloud = true
		}
	}
	connected := map[string]bool{}
	var vms []modelVM
	for _, m := range ms {
		var names []string
		for _, id := range m.BackendIDs {
			names = append(names, bmap[id].Name)
		}
		vms = append(vms, modelVM{Model: m, BackendNames: strings.Join(names, ", ")})
		connected[m.Alias] = true
		if m.UpstreamName != "" {
			connected[m.UpstreamName] = true
		}
	}
	raw := u.health.Catalog(bs)
	var cat []catalogVM
	available := 0
	for _, e := range raw {
		loaded := map[string]bool{}
		for _, n := range e.LoadedOn {
			loaded[n] = true
		}
		item := catalogVM{
			CatalogEntry: e, Connected: connected[e.Name],
			BackendLabel: strings.Join(e.BackendNames, ", "),
			SizeLabel:    humanSize(e.Size),
			LoadedLabel:  strings.Join(e.LoadedOn, ", "),
			CtxLabel:     domain.FormatContext(e.Context),
			PriceLabel:   domain.PriceLabel(e.Priced, e.PromptUSD, e.CompletionUSD),
			PriceBand:    domain.PriceBand(e.Priced, e.PromptUSD),
		}
		for i, id := range e.BackendIDs {
			name := e.BackendNames[i]
			short := strings.TrimPrefix(name, "mac-")
			b := bmap[id]
			item.Hosts = append(item.Hosts, catalogHost{
				ID: id, Name: name, Short: short, Loaded: loaded[name],
				Kind: b.KindNorm(), Label: b.Label(), CanLoad: b.CanLoad(),
				CanDelete: b.CanDelete(), Cloud: b.Cloud(), Hint: b.UIHint(),
			})
		}
		if !item.Connected {
			available++
		}
		cat = append(cat, item)
	}
	detected := map[string]int{}
	byName := map[string]domain.CatalogEntry{}
	provSet := map[string]bool{}
	for _, e := range raw {
		detected[e.Name] = e.Context
		byName[e.Name] = e
		if e.Provider != "" {
			provSet[e.Provider] = true
		}
	}
	for i := range vms {
		d := detected[vms[i].UpstreamName]
		if d == 0 {
			d = detected[vms[i].Alias]
		}
		vms[i].Detected = d
		vms[i].CtxLabel = domain.FormatContext(vms[i].ContextWindow(d))
		meta := byName[vms[i].UpstreamName]
		if meta.Name == "" {
			meta = byName[vms[i].Alias]
		}
		vms[i].Provider = meta.Provider
		vms[i].PriceLabel = domain.PriceLabel(meta.Priced, meta.PromptUSD, meta.CompletionUSD)
		if vms[i].Provider != "" {
			provSet[vms[i].Provider] = true
		}
		fb := vms[i].Fallback
		if fb != "" {
			for _, o := range vms {
				if o.Alias == fb {
					vms[i].FallbackOK = true
					break
				}
			}
		}
	}
	providers := make([]string, 0, len(provSet))
	for p := range provSet {
		providers = append(providers, p)
	}
	sort.Strings(providers)
	u.render(w, "models", map[string]any{
		"Title": "Модели", "Nav": "models", "Models": vms, "Backends": bs,
		"PullBackends": pullers, "HasVLLM": hasVLLM, "HasCloud": hasCloud,
		"Catalog": cat, "Available": available, "Providers": providers,
		"Flash": flashMsg(r.URL.Query().Get("ok")), "Error": errMsg(r.URL.Query().Get("err")),
	})
}

func (u *UI) connectModels(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	bs, _ := u.st.ListBackends()
	cat := u.health.Catalog(bs)
	byName := map[string]domain.CatalogEntry{}
	for _, e := range cat {
		byName[e.Name] = e
	}
	n := 0
	for _, name := range r.Form["model"] {
		e, ok := byName[name]
		if !ok || len(e.BackendIDs) == 0 {
			continue
		}
		if err := u.st.ConnectOllamaModel(name, e.BackendIDs, r.FormValue("lb_policy"), e.Context); err != nil {
			http.Redirect(w, r, "/admin/models?err="+err.Error(), http.StatusFound)
			return
		}
		n++
	}
	if n == 0 {
		http.Redirect(w, r, "/admin/models?err=select_models", http.StatusFound)
		return
	}
	http.Redirect(w, r, "/admin/models?ok=connected", http.StatusFound)
}

func (u *UI) saveModel(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	upstream := strings.TrimSpace(r.FormValue("upstream_name"))
	alias := strings.TrimSpace(r.FormValue("alias"))
	if alias == "" {
		alias = upstream
	}
	maxCtx, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("max_context")))
	m := domain.Model{
		Alias:        alias,
		UpstreamName: upstream,
		LBPolicy:     r.FormValue("lb_policy"),
		Enabled:      true,
		MaxContext:   maxCtx,
		Fallback:     strings.TrimSpace(r.FormValue("fallback")),
	}
	for _, v := range r.Form["backend_id"] {
		id, _ := strconv.ParseInt(v, 10, 64)
		if id > 0 {
			m.BackendIDs = append(m.BackendIDs, id)
		}
	}
	if m.Alias == "" {
		http.Redirect(w, r, "/admin/models?err=select_models", http.StatusFound)
		return
	}
	if len(m.BackendIDs) == 0 {
		bs, _ := u.st.ListBackends()
		for _, e := range u.health.Catalog(bs) {
			if e.Name == m.UpstreamName || e.Name == m.Alias {
				m.BackendIDs = e.BackendIDs
				if m.UpstreamName == "" {
					m.UpstreamName = e.Name
				}
				break
			}
		}
	}
	if len(m.BackendIDs) == 0 {
		http.Redirect(w, r, "/admin/models?err=no_backend", http.StatusFound)
		return
	}
	if existing, err := u.st.GetModelByAlias(m.Alias); err == nil {
		m.ID = existing.ID
	}
	if _, err := u.st.SaveModel(m); err != nil {
		http.Redirect(w, r, "/admin/models?err="+err.Error(), http.StatusFound)
		return
	}
	http.Redirect(w, r, "/admin/models?ok=saved", http.StatusFound)
}

func (u *UI) delModel(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	_ = u.st.DeleteModel(id)
	http.Redirect(w, r, "/admin/models?ok=deleted", http.StatusFound)
}

func (u *UI) setModelContext(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	_ = r.ParseForm()
	n, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("max_context")))
	if n < 0 {
		n = 0
	}
	m, err := u.st.GetModel(id)
	if err != nil {
		http.Redirect(w, r, "/admin/models?err="+err.Error(), http.StatusFound)
		return
	}
	m.MaxContext = n
	if _, err := u.st.SaveModel(m); err != nil {
		http.Redirect(w, r, "/admin/models?err="+err.Error(), http.StatusFound)
		return
	}
	http.Redirect(w, r, "/admin/models?ok=context_saved", http.StatusFound)
}

func (u *UI) setModelFallback(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	_ = r.ParseForm()
	m, err := u.st.GetModel(id)
	if err != nil {
		http.Redirect(w, r, "/admin/models?err="+err.Error(), http.StatusFound)
		return
	}
	m.Fallback = strings.TrimSpace(r.FormValue("fallback"))
	if _, err := u.st.SaveModel(m); err != nil {
		http.Redirect(w, r, "/admin/models?err="+err.Error(), http.StatusFound)
		return
	}
	http.Redirect(w, r, "/admin/models?ok=fallback_saved", http.StatusFound)
}

type keyVM struct {
	domain.APIKey
	Allowed   string
	LastUsed  string
	AllModels bool
}

func (u *UI) keysPage(w http.ResponseWriter, r *http.Request) {
	ks, _ := u.st.ListKeys()
	var vms []keyVM
	for _, k := range ks {
		all := keyAllowsAll(k)
		last := "—"
		if k.LastUsedAt != nil && !k.LastUsedAt.IsZero() {
			last = k.LastUsedAt.Local().Format("02.01 15:04")
		}
		vms = append(vms, keyVM{
			APIKey: k, Allowed: strings.Join(k.AllowedModels, ", "),
			LastUsed: last, AllModels: all,
		})
	}
	var edit *keyVM
	if id, _ := strconv.ParseInt(r.URL.Query().Get("edit"), 10, 64); id > 0 {
		for i := range vms {
			if vms[i].ID == id {
				e := vms[i]
				edit = &e
				break
			}
		}
	}
	selected := map[string]bool{}
	if edit != nil && !edit.AllModels {
		for _, n := range edit.AllowedModels {
			selected[n] = true
		}
	}
	opts, providers := u.keyModelOpts(selected)
	u.render(w, "keys", map[string]any{
		"Title": "Ключи", "Nav": "keys", "Keys": vms, "KeyModels": opts, "Providers": providers,
		"EditKey": edit, "NewKey": r.URL.Query().Get("new"),
		"Flash": flashMsg(r.URL.Query().Get("ok")), "Error": errMsg(r.URL.Query().Get("err")),
	})
}

type keyModelOpt struct {
	Name, Group, GroupHead, CtxLabel, Provider, PriceLabel, PriceBand, Title string
	Prompt                                                                   float64
	Checked                                                                  bool
}

func (u *UI) keyModelOpts(selected map[string]bool) ([]keyModelOpt, []string) {
	var out []keyModelOpt
	seen := map[string]bool{}
	ms, _ := u.st.ListModels()
	bs, _ := u.st.ListBackends()
	cat := u.health.Catalog(bs)
	byName := map[string]domain.CatalogEntry{}
	for _, e := range cat {
		byName[e.Name] = e
	}
	provSet := map[string]bool{}
	add := func(name, group, head string, ctx int, meta domain.CatalogEntry) bool {
		if name == "" || seen[name] {
			return false
		}
		seen[name] = true
		if ctx == 0 {
			ctx = meta.Context
		}
		if meta.Provider != "" {
			provSet[meta.Provider] = true
		}
		out = append(out, keyModelOpt{
			Name: name, Group: group, GroupHead: head,
			CtxLabel: domain.FormatContext(ctx), Provider: meta.Provider,
			PriceLabel: domain.PriceLabel(meta.Priced, meta.PromptUSD, meta.CompletionUSD),
			PriceBand:  domain.PriceBand(meta.Priced, meta.PromptUSD),
			Title:      meta.Title, Prompt: meta.PromptUSD, Checked: selected[name],
		})
		return true
	}
	head := "Alias в шлюзе"
	for _, m := range ms {
		if !m.Enabled || m.Alias == "" {
			continue
		}
		meta := byName[m.UpstreamName]
		if meta.Name == "" {
			meta = byName[m.Alias]
		}
		if add(m.Alias, "шлюз", head, m.ContextWindow(meta.Context), meta) {
			head = ""
		}
	}
	head = "На серверах"
	for _, e := range cat {
		if add(e.Name, strings.Join(e.BackendNames, ", "), head, e.Context, e) {
			head = ""
		}
	}
	providers := make([]string, 0, len(provSet))
	for p := range provSet {
		providers = append(providers, p)
	}
	sort.Strings(providers)
	return out, providers
}

func keyAllowsAll(k domain.APIKey) bool {
	if len(k.AllowedModels) == 0 {
		return true
	}
	for _, m := range k.AllowedModels {
		if m == "*" {
			return true
		}
	}
	return false
}

func parseKeyAllowlist(r *http.Request) ([]string, bool) {
	if r.FormValue("all_models") == "1" {
		return []string{"*"}, true
	}
	var models []string
	for _, p := range r.Form["model"] {
		p = strings.TrimSpace(p)
		if p != "" {
			models = append(models, p)
		}
	}
	return models, len(models) > 0
}

func (u *UI) createKey(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	plain, prefix, hash, err := auth.GenerateKey()
	if err != nil {
		http.Redirect(w, r, "/admin/keys?err="+err.Error(), http.StatusFound)
		return
	}
	models, ok := parseKeyAllowlist(r)
	if !ok {
		http.Redirect(w, r, "/admin/keys?err=select_models", http.StatusFound)
		return
	}
	rpm, _ := strconv.Atoi(r.FormValue("rpm"))
	k := domain.APIKey{
		Name: strings.TrimSpace(r.FormValue("name")), Prefix: prefix,
		KeyHash: hash, AllowedModels: models, RPM: rpm, Enabled: true,
	}
	if k.Name == "" {
		k.Name = "key"
	}
	if _, err := u.st.InsertKey(k); err != nil {
		http.Redirect(w, r, "/admin/keys?err="+err.Error(), http.StatusFound)
		return
	}
	http.Redirect(w, r, "/admin/keys?new="+plain, http.StatusFound)
}

func (u *UI) updateKey(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	k, err := u.st.GetKey(id)
	if err != nil {
		http.Redirect(w, r, "/admin/keys?err=unknown+key", http.StatusFound)
		return
	}
	_ = r.ParseForm()
	models, ok := parseKeyAllowlist(r)
	if !ok {
		http.Redirect(w, r, "/admin/keys?edit="+strconv.FormatInt(id, 10)+"&err=select_models", http.StatusFound)
		return
	}
	if name := strings.TrimSpace(r.FormValue("name")); name != "" {
		k.Name = name
	}
	rpm, _ := strconv.Atoi(r.FormValue("rpm"))
	k.RPM = rpm
	k.AllowedModels = models
	if err := u.st.UpdateKey(k); err != nil {
		http.Redirect(w, r, "/admin/keys?err="+err.Error(), http.StatusFound)
		return
	}
	http.Redirect(w, r, "/admin/keys?ok=key_updated", http.StatusFound)
}

func (u *UI) delKey(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	_ = u.st.DeleteKey(id)
	http.Redirect(w, r, "/admin/keys?ok=revoked", http.StatusFound)
}

func (u *UI) logs(w http.ResponseWriter, r *http.Request) {
	ls, _ := u.st.ListLogs(200)
	u.render(w, "logs", map[string]any{"Title": "Лог", "Nav": "logs", "Logs": ls})
}

func flashMsg(code string) string {
	switch code {
	case "connected":
		return "Модели подключены."
	case "saved":
		return "Сохранено."
	case "deleted":
		return "Удалено."
	case "revoked":
		return "Ключ отозван."
	case "refreshed":
		return "Список моделей обновлён."
	case "backend_saved":
		return "Сервер добавлен. Обновите модели, если список пустой."
	case "backend_deleted":
		return "Бэкенд удалён."
	case "password_updated":
		return "Пароль админки изменён."
	case "loaded":
		return "Модель загружена в RAM."
	case "loading":
		return "Загрузка в RAM идёт в фоне. Полоса останется после обновления страницы."
	case "pulling":
		return "Скачивание идёт в фоне. Полоса останется после обновления страницы."
	case "unloaded":
		return "Модель выгружена из RAM."
	case "busy":
		return "На этом сервере уже идёт другая задача."
	case "key_updated":
		return "Доступ ключа обновлён."
	case "context_saved":
		return "Контекст модели сохранён."
	case "fallback_saved":
		return "Запасная модель сохранена."
	default:
		return code
	}
}

func errMsg(code string) string {
	switch code {
	case "select_models":
		return "Выберите хотя бы одну модель."
	case "no_backend":
		return "Нет живого сервера с этой моделью. Нажмите «Обновить»."
	case "name+and+url+required":
		return "Нужны имя и URL."
	case "token+required":
		return "Для OpenRouter и Ollama Cloud нужен API-ключ."
	case "min+6+chars":
		return "Пароль не короче 6 символов."
	default:
		return code
	}
}

func (u *UI) runningPull() *domain.Job {
	if u.jobs == nil {
		return nil
	}
	for _, j := range u.jobs.List() {
		if j.Kind == "pull" && j.Status == "running" {
			jp := j
			return &jp
		}
	}
	return nil
}

func (u *UI) render(w http.ResponseWriter, name string, data map[string]any) {
	t := u.pages[name]
	if t == nil {
		http.Error(w, "template "+name, 500)
		return
	}
	if data == nil {
		data = map[string]any{}
	}
	if _, ok := data["PullJob"]; !ok {
		data["PullJob"] = u.runningPull()
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "layout.html", data); err != nil {
		http.Error(w, err.Error(), 500)
	}
}
