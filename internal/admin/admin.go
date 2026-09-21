package admin

import (
	"fmt"
	"html/template"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/javded-itres/mikrollm/internal/auth"
	"github.com/javded-itres/mikrollm/internal/domain"
	"github.com/javded-itres/mikrollm/internal/hubclient"
	"github.com/javded-itres/mikrollm/internal/ports"
	"github.com/javded-itres/mikrollm/internal/queue"
	"github.com/javded-itres/mikrollm/internal/web"
)

type HubClient interface {
	Status() (state, err string)
	URL() string
	Kick()
	RefreshCatalog()
	Peers() (selfID string, peers []domain.HubPeer)
}

type Deps struct {
	Store  ports.Store
	Health ports.Health
	Auth   ports.Auth
	Host   ports.Host
	Jobs   ports.Jobs
	Chat   ports.ChatGateway
	Queues *queue.Engine
	Hub    HubClient
}

type UI struct {
	st     ports.Store
	health ports.Health
	keys   ports.Auth
	host   ports.Host
	jobs   ports.Jobs
	chat   ports.ChatGateway
	queues *queue.Engine
	hub    HubClient
	pages  map[string]*template.Template
	login  *template.Template
}

func New(d Deps) *UI {
	fm := template.FuncMap{
		"hsize": humanSize,
		"hctx":  domain.FormatContext,
		"join":  strings.Join,
		"add":   func(a, b int) int { return a + b },
		"hastarget": func(ts []domain.PolicyTarget, kind, key string) bool {
			return hasTarget(ts, kind, key)
		},
		"usd": domain.FormatUSD,
		"hasval": func(ss []string, v string) bool {
			for _, s := range ss {
				if s == v {
					return true
				}
			}
			return false
		},
	}
	must := func(files ...string) *template.Template {
		return template.Must(template.New("layout.html").Funcs(fm).ParseFS(web.FS, files...))
	}
	return &UI{
		st: d.Store, health: d.Health, keys: d.Auth, host: d.Host, jobs: d.Jobs, chat: d.Chat, queues: d.Queues, hub: d.Hub,
		login: template.Must(template.New("login.html").Funcs(fm).ParseFS(web.FS, "templates/login.html")),
		pages: map[string]*template.Template{
			"dash":     must("templates/layout.html", "templates/dash.html"),
			"models":   must("templates/layout.html", "templates/models.html"),
			"keys":     must("templates/layout.html", "templates/keys.html"),
			"chat":     must("templates/layout.html", "templates/chat.html"),
			"logs":     must("templates/layout.html", "templates/logs.html"),
			"queues":   must("templates/layout.html", "templates/queues.html"),
			"security": must("templates/layout.html", "templates/security.html"),
			"billing":  must("templates/layout.html", "templates/billing.html"),
		},
	}
}

func (u *UI) Mount(mux *http.ServeMux) {
	mux.Handle("GET /admin/static/", staticHandler())
	mux.HandleFunc("GET /admin/manifest.webmanifest", pwaManifest)
	mux.HandleFunc("GET /admin/sw.js", pwaServiceWorker)
	mux.HandleFunc("GET /admin/login", u.loginPage)
	mux.HandleFunc("POST /admin/login", u.loginPost)
	mux.HandleFunc("POST /admin/logout", u.protect(u.logout))
	mux.HandleFunc("GET /admin", u.protect(u.dash))
	mux.HandleFunc("POST /admin/refresh", u.protect(u.refresh))
	mux.HandleFunc("POST /admin/backends/{id}/refresh-models", u.protect(u.refreshBackendModels))
	mux.HandleFunc("POST /admin/backends", u.protect(u.addBackend))
	mux.HandleFunc("POST /admin/backends/{id}/enabled", u.protect(u.setBackendEnabled))
	mux.HandleFunc("POST /admin/backends/{id}/delete", u.protect(u.delBackend))
	mux.HandleFunc("POST /admin/password", u.protect(u.password))
	mux.HandleFunc("POST /admin/mcp/token", u.protect(u.rotateMCP))
	mux.HandleFunc("POST /admin/hub", u.protect(u.saveHub))
	mux.HandleFunc("POST /admin/models/{id}/hub-share", u.protect(u.setModelHubShare))
	mux.HandleFunc("POST /admin/models/hub-bulk", u.protect(u.bulkHubShare))
	mux.HandleFunc("GET /admin/models", u.protect(u.models))
	mux.HandleFunc("POST /admin/models", u.protect(u.saveModel))
	mux.HandleFunc("POST /admin/models/connect", u.protect(u.connectModels))
	mux.HandleFunc("POST /admin/models/{id}/delete", u.protect(u.delModel))
	mux.HandleFunc("POST /admin/models/{id}/context", u.protect(u.setModelContext))
	mux.HandleFunc("POST /admin/models/{id}/fallback", u.protect(u.setModelFallback))
	mux.HandleFunc("POST /admin/models/{id}/prompt-cache", u.protect(u.setModelPromptCache))
	mux.HandleFunc("POST /admin/prompt-cache", u.protect(u.setPromptCache))
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
	mux.HandleFunc("POST /admin/images", u.protect(u.imagesPost))
	mux.HandleFunc("POST /admin/videos", u.protect(u.videosPost))
	mux.HandleFunc("GET /admin/videos/{id}", u.protect(u.videoStatus))
	mux.HandleFunc("GET /admin/videos/{id}/content", u.protect(u.videoContent))
	mux.HandleFunc("GET /admin/model-params", u.protect(u.modelParams))
	mux.HandleFunc("GET /admin/logs", u.protect(u.logs))
	mux.HandleFunc("GET /admin/billing", u.protect(u.billing))
	mux.HandleFunc("GET /admin/security", u.protect(u.securityPage))
	mux.HandleFunc("POST /admin/security", u.protect(u.savePolicy))
	mux.HandleFunc("POST /admin/security/{id}/delete", u.protect(u.delPolicy))
	mux.HandleFunc("POST /admin/security/{id}/enabled", u.protect(u.setPolicyEnabled))
	mux.HandleFunc("GET /admin/queues", u.protect(u.queuesPage))
	mux.HandleFunc("GET /admin/queues/live", u.protect(u.queuesLive))
	mux.HandleFunc("POST /admin/queues", u.protect(u.saveQueue))
	mux.HandleFunc("POST /admin/queues/{id}/delete", u.protect(u.delQueue))
	mux.HandleFunc("POST /admin/queues/{id}/steps", u.protect(u.addQueueStep))
	mux.HandleFunc("POST /admin/queues/{id}/steps/{pos}/up", u.protect(u.upQueueStep))
	mux.HandleFunc("POST /admin/queues/{id}/steps/{pos}/delete", u.protect(u.delQueueStep))
	mux.HandleFunc("POST /admin/queues/{id}/aliases", u.protect(u.addQueueAlias))
	mux.HandleFunc("POST /admin/queues/{id}/aliases/delete", u.protect(u.delQueueAlias))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/admin", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	})
}

func (u *UI) loginPage(w http.ResponseWriter, r *http.Request) {
	if u.keys.ValidSession(r) {
		http.Redirect(w, r, "/admin", http.StatusFound)
		return
	}
	_ = u.login.Execute(w, map[string]any{"Error": r.URL.Query().Get("err")})
}

func (u *UI) loginPost(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
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
		if !b.Enabled {
			status = "Выкл"
		} else if st.Healthy {
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
	var qviews []domain.QueueView
	if u.queues != nil {
		qviews = u.queues.Snapshot()
	}
	waitN := 0
	for _, q := range qviews {
		waitN += q.Waiting + q.Running
	}
	prefix, _ := u.st.MCPTokenPrefix()
	newMCP := ""
	okCode := r.URL.Query().Get("ok")
	if okCode == "mcp_token" {
		newMCP = u.keys.TakeFlash(w, r)
	}
	flash := flashMsg(okCode)
	if newMCP != "" {
		flash = ""
	}
	var cacheTok int
	var savedUSD, billedUSD float64
	if ls, err := u.st.ListLogs(500); err == nil {
		for _, l := range ls {
			cacheTok += l.CachedTokens
			savedUSD += l.SavedUSD
			billedUSD += l.UsageCost
		}
	}
	hubCfg, _ := u.st.HubSettings()
	hubURL := hubclient.HubURL()
	hubState, hubErr := "off", ""
	if u.hub != nil {
		hubState, hubErr = u.hub.Status()
		if u.hub.URL() != "" {
			hubURL = u.hub.URL()
		}
	}
	hubLabel := "выкл"
	switch hubState {
	case "online":
		hubLabel = "в сети"
	case "error":
		hubLabel = "ошибка"
	}
	if hubCfg.Name == "" {
		hubCfg.Name, _ = os.Hostname()
	}
	hubAutoNode, hubAutoModel := "", ""
	if autoM, err := u.st.GetModelByAlias(domain.HubAutoAlias); err == nil && domain.IsHubAuto(autoM) {
		hubAutoNode, hubAutoModel = autoM.HubNodeName, autoM.UpstreamName
		if hubAutoNode == "" {
			hubAutoNode = autoM.HubNodeID
		}
	}
	u.render(w, r, "dash", map[string]any{
		"Title": "Статус", "Nav": "dash", "Backends": vms,
		"Up": up, "Total": len(vms), "AliasCount": len(aliases),
		"Queues": qviews, "QueueLive": waitN,
		"CacheTokens": cacheTok, "SavedUSD": savedUSD, "UsageCost": billedUSD,
		"MCPPrefix": prefix, "NewMCPToken": newMCP,
		"HubURL": hubURL, "HubEnabled": hubCfg.Enabled, "HubName": hubCfg.Name,
		"HubNodeID": hubCfg.NodeID, "HubState": hubState, "HubLabel": hubLabel, "HubError": hubErr,
		"HubAutoNode": hubAutoNode, "HubAutoModel": hubAutoModel,
		"HubSchedule": hubCfg.Schedule, "HubSharingNow": hubCfg.Schedule.SharingAt(time.Now()),
		"HubShareDays": shareDaySet(hubCfg.Schedule.Days),
		"HubTZ":        hubTZ(hubCfg.Schedule.TZ),
		"Flash":        flash, "Error": errMsg(r.URL.Query().Get("err")),
	})
}

func (u *UI) saveHub(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	cfg, err := u.st.HubSettings()
	if err != nil {
		http.Redirect(w, r, "/admin?err="+err.Error(), http.StatusFound)
		return
	}
	cfg.Name = strings.TrimSpace(r.FormValue("name"))
	if cfg.Name == "" {
		cfg.Name, _ = os.Hostname()
	}
	cfg.Enabled = parseEnabled(r.FormValue("enabled"))
	cfg.Schedule = domain.HubSchedule{
		Enabled: parseEnabled(r.FormValue("share_sched")),
		Days:    domain.ParseShareDays(r.Form["share_day"]),
		Start:   strings.TrimSpace(r.FormValue("share_start")),
		End:     strings.TrimSpace(r.FormValue("share_end")),
		TZ:      strings.TrimSpace(r.FormValue("share_tz")),
	}
	if cfg.Schedule.TZ == "" {
		cfg.Schedule.TZ = "Europe/Moscow"
	}
	if err := u.st.SetHubSettings(cfg); err != nil {
		http.Redirect(w, r, "/admin?err="+err.Error(), http.StatusFound)
		return
	}
	if u.hub != nil {
		u.hub.Kick()
	}
	http.Redirect(w, r, "/admin?ok=hub_saved", http.StatusFound)
}

func (u *UI) setModelHubShare(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	_ = r.ParseForm()
	m, err := u.st.GetModel(id)
	if err != nil {
		http.Redirect(w, r, "/admin/models?err="+err.Error(), http.StatusFound)
		return
	}
	if domain.IsHubAuto(m) {
		http.Redirect(w, r, "/admin/models?err=reserved_auto", http.StatusFound)
		return
	}
	m.HubShare = parseEnabled(r.FormValue("hub_share"))
	if _, err := u.st.SaveModel(m); err != nil {
		http.Redirect(w, r, "/admin/models?err="+err.Error(), http.StatusFound)
		return
	}
	if u.hub != nil {
		u.hub.Kick()
	}
	http.Redirect(w, r, "/admin/models?ok=hub_share_saved", http.StatusFound)
}

func (u *UI) bulkHubShare(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	share := parseEnabled(r.FormValue("hub_share"))
	n := 0
	for _, raw := range r.Form["id"] {
		id, _ := strconv.ParseInt(raw, 10, 64)
		if id <= 0 {
			continue
		}
		m, err := u.st.GetModel(id)
		if err != nil || m.HubNodeID != "" {
			continue
		}
		m.HubShare = share
		if _, err := u.st.SaveModel(m); err != nil {
			http.Redirect(w, r, "/admin/models?err="+err.Error(), http.StatusFound)
			return
		}
		n++
	}
	if u.hub != nil {
		u.hub.Kick()
	}
	if n == 0 {
		http.Redirect(w, r, "/admin/models?err=select_models", http.StatusFound)
		return
	}
	http.Redirect(w, r, "/admin/models?ok=hub_share_saved", http.StatusFound)
}

func (u *UI) refresh(w http.ResponseWriter, r *http.Request) {
	if r.FormValue("full") == "1" {
		u.health.RefreshAll()
	} else {
		u.health.CheckOnce()
	}
	http.Redirect(w, r, safeAdminPath(r.FormValue("next"), "/admin")+"?ok=refreshed", http.StatusFound)
}

func (u *UI) refreshBackendModels(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if id > 0 {
		u.health.RefreshBackend(id)
	}
	http.Redirect(w, r, safeAdminPath(r.FormValue("next"), "/admin")+"?ok=catalog_refreshed", http.StatusFound)
}

func (u *UI) addBackend(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	name := strings.TrimSpace(r.FormValue("name"))
	kind := strings.TrimSpace(r.FormValue("kind"))
	token := strings.TrimSpace(r.FormValue("token"))
	url, err := domain.SanitizeBackendURL(kind, r.FormValue("base_url"))
	if name == "" || err != nil || url == "" {
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

func shareDaySet(days []int) map[string]bool {
	out := map[string]bool{}
	for _, d := range days {
		out[strconv.Itoa(d)] = true
	}
	return out
}

func hubTZ(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "Europe/Moscow"
	}
	return v
}

func parseEnabled(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "on", "yes":
		return true
	default:
		return false
	}
}

func (u *UI) setBackendEnabled(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if id <= 0 {
		http.Redirect(w, r, "/admin?err=no_backend", http.StatusFound)
		return
	}
	on := parseEnabled(r.FormValue("enabled"))
	if err := u.st.SetBackendEnabled(id, on); err != nil {
		http.Redirect(w, r, "/admin?err=no_backend", http.StatusFound)
		return
	}
	go u.health.CheckOnce()
	ok := "backend_disabled"
	if on {
		ok = "backend_enabled"
	}
	http.Redirect(w, r, "/admin?ok="+ok, http.StatusFound)
}

func (u *UI) delBackend(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	_ = u.st.DeleteBackend(id)
	http.Redirect(w, r, "/admin?ok=backend_deleted", http.StatusFound)
}

func (u *UI) rotateMCP(w http.ResponseWriter, r *http.Request) {
	plain, _, _, err := auth.GenerateMCPToken()
	if err != nil {
		http.Redirect(w, r, "/admin?err="+err.Error(), http.StatusFound)
		return
	}
	if _, err := u.st.SetMCPToken(plain); err != nil {
		http.Redirect(w, r, "/admin?err="+err.Error(), http.StatusFound)
		return
	}
	u.keys.PutFlash(w, plain)
	http.Redirect(w, r, "/admin?ok=mcp_token", http.StatusFound)
}

func (u *UI) password(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	pw := r.FormValue("password")
	if len(pw) < 8 {
		http.Redirect(w, r, "/admin?err=min+8+chars", http.StatusFound)
		return
	}
	if err := u.st.SetAdminPassword(pw); err != nil {
		http.Redirect(w, r, "/admin?err="+err.Error(), http.StatusFound)
		return
	}
	_ = u.keys.IssueCookie(w, r)
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
	Hub       bool
	Online    bool
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
	CheckValue   string
	ServersCSV   string
	Hosts        []catalogHost
}

type serverOpt struct {
	Value, Label, Group string
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
	hubConnected := map[string]bool{}
	var vms []modelVM
	for _, m := range ms {
		var names []string
		for _, id := range m.BackendIDs {
			names = append(names, bmap[id].Name)
		}
		if m.HubNodeID != "" {
			if m.HubNodeName != "" {
				names = []string{m.HubNodeName}
			} else {
				names = []string{"hub"}
			}
			hubConnected[m.HubNodeID+"|"+m.UpstreamName] = true
		}
		vm := modelVM{Model: m, BackendNames: strings.Join(names, ", ")}
		if len(vm.Media) == 0 {
			vm.Media = domain.InferMedia(m.Alias, nil)
			vm.Media = domain.MergeMedia(vm.Media, domain.InferMedia(m.UpstreamName, nil))
		}
		vms = append(vms, vm)
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
		on := connected[e.Name]
		check := e.Name
		servers := e.BackendNames
		if e.HubNodeID != "" {
			on = hubConnected[e.HubNodeID+"|"+e.Name]
			check = "hub|" + e.HubNodeID + "|" + e.Name
			servers = []string{e.HubNodeName}
			if e.HubNodeName == "" {
				servers = []string{e.HubNodeID}
			}
		}
		item := catalogVM{
			CatalogEntry: e, Connected: on, CheckValue: check,
			BackendLabel: strings.Join(e.BackendNames, ", "),
			SizeLabel:    humanSize(e.Size),
			LoadedLabel:  strings.Join(e.LoadedOn, ", "),
			CtxLabel:     domain.FormatContext(e.Context),
			PriceLabel:   domain.FormatCatalogPrice(e.Priced, e.PromptUSD, e.CompletionUSD, e.ImageUSD, e.ImageTokUSD, e.VideoSecUSD),
			PriceBand:    domain.PriceBand(e.Priced, domain.PriceBandValue(e.PromptUSD, e.ImageUSD, e.ImageTokUSD, e.VideoSecUSD)),
			ServersCSV:   strings.Join(servers, ","),
		}
		if e.HubNodeID != "" {
			short := e.HubNodeName
			if short == "" {
				short = e.HubNodeID
				if len(short) > 8 {
					short = short[:8]
				}
			}
			item.Hosts = append(item.Hosts, catalogHost{
				Name: e.HubNodeName, Short: short, Kind: domain.KindHub, Label: "Hub",
				Hub: true, Online: e.HubOnline, Hint: "Модель на другом MikroLLM через hub",
			})
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
		vms[i].PriceLabel = domain.FormatCatalogPrice(meta.Priced, meta.PromptUSD, meta.CompletionUSD, meta.ImageUSD, meta.ImageTokUSD, meta.VideoSecUSD)
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
	var servers []serverOpt
	for _, b := range bs {
		if !b.Enabled {
			continue
		}
		servers = append(servers, serverOpt{Value: b.Name, Label: b.Name + " · " + b.Label(), Group: "local"})
	}
	if u.hub != nil {
		_, peers := u.hub.Peers()
		for _, p := range peers {
			lb := p.Name
			if p.Online {
				lb += " · онлайн"
			} else {
				lb += " · офлайн"
			}
			servers = append(servers, serverOpt{Value: p.Name, Label: lb, Group: "hub"})
		}
	}
	u.render(w, r, "models", map[string]any{
		"Title": "Модели", "Nav": "models", "Models": vms, "Backends": bs,
		"PullBackends": pullers, "HasVLLM": hasVLLM, "HasCloud": hasCloud,
		"Catalog": cat, "Available": available, "Providers": providers, "Servers": servers,
		"PromptCache": u.st.PromptCacheMode(),
		"Flash":       flashMsg(r.URL.Query().Get("ok")), "Error": errMsg(r.URL.Query().Get("err")),
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
		if strings.HasPrefix(name, "hub|") {
			parts := strings.SplitN(strings.TrimPrefix(name, "hub|"), "|", 2)
			if len(parts) != 2 {
				continue
			}
			nodeID, alias := parts[0], parts[1]
			var e domain.CatalogEntry
			for _, c := range cat {
				if c.HubNodeID == nodeID && c.Name == alias {
					e = c
					break
				}
			}
			if e.HubNodeID == "" {
				continue
			}
			if err := u.st.ConnectHubModel(alias, e.HubNodeID, e.HubNodeName, e.Context, e.Media); err != nil {
				http.Redirect(w, r, "/admin/models?err="+err.Error(), http.StatusFound)
				return
			}
			n++
			continue
		}
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
	if strings.EqualFold(m.Alias, domain.HubAutoAlias) {
		http.Redirect(w, r, "/admin/models?err=reserved_auto", http.StatusFound)
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
				if m.HubNodeID == "" {
					m.HubNodeID = e.HubNodeID
					m.HubNodeName = e.HubNodeName
				}
				if len(m.Media) == 0 {
					m.Media = e.Media
				}
				if len(m.BackendIDs) > 0 {
					break
				}
			}
		}
	}
	if existing, err := u.st.GetModelByAlias(m.Alias); err == nil {
		m.ID = existing.ID
		m.HubShare = existing.HubShare
		if m.PromptCache == "" {
			m.PromptCache = existing.PromptCache
		}
		if len(m.Media) == 0 {
			m.Media = existing.Media
		}
		if m.HubNodeID == "" {
			m.HubNodeID = existing.HubNodeID
			m.HubNodeName = existing.HubNodeName
		}
	}
	if len(m.BackendIDs) == 0 && m.HubNodeID == "" {
		http.Redirect(w, r, "/admin/models?err=no_backend", http.StatusFound)
		return
	}
	if _, err := u.st.SaveModel(m); err != nil {
		http.Redirect(w, r, "/admin/models?err="+err.Error(), http.StatusFound)
		return
	}
	http.Redirect(w, r, "/admin/models?ok=saved", http.StatusFound)
}

func (u *UI) delModel(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if m, err := u.st.GetModel(id); err == nil && domain.IsHubAuto(m) {
		http.Redirect(w, r, "/admin/models?err=reserved_auto", http.StatusFound)
		return
	}
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

func (u *UI) setPromptCache(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if err := u.st.SetPromptCacheMode(r.FormValue("prompt_cache")); err != nil {
		http.Redirect(w, r, "/admin/models?err="+err.Error(), http.StatusFound)
		return
	}
	http.Redirect(w, r, "/admin/models?ok=prompt_cache_saved", http.StatusFound)
}

func (u *UI) setModelPromptCache(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	_ = r.ParseForm()
	m, err := u.st.GetModel(id)
	if err != nil {
		http.Redirect(w, r, "/admin/models?err="+err.Error(), http.StatusFound)
		return
	}
	v := strings.ToLower(strings.TrimSpace(r.FormValue("prompt_cache")))
	switch v {
	case "inherit", "off", "auto", "on":
		m.PromptCache = v
	default:
		http.Redirect(w, r, "/admin/models?err=prompt_cache+must+be+inherit|off|auto|on", http.StatusFound)
		return
	}
	if _, err := u.st.SaveModel(m); err != nil {
		http.Redirect(w, r, "/admin/models?err="+err.Error(), http.StatusFound)
		return
	}
	http.Redirect(w, r, "/admin/models?ok=prompt_cache_saved", http.StatusFound)
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
	u.render(w, r, "keys", map[string]any{
		"Title": "Ключи", "Nav": "keys", "Keys": vms, "KeyModels": opts, "Providers": providers,
		"EditKey": edit, "NewKey": u.keys.TakeFlash(w, r),
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
			PriceLabel: domain.FormatCatalogPrice(meta.Priced, meta.PromptUSD, meta.CompletionUSD, meta.ImageUSD, meta.ImageTokUSD, meta.VideoSecUSD),
			PriceBand:  domain.PriceBand(meta.Priced, domain.PriceBandValue(meta.PromptUSD, meta.ImageUSD, meta.ImageTokUSD, meta.VideoSecUSD)),
			Title:      meta.Title, Prompt: meta.PromptUSD, Checked: selected[name],
		})
		return true
	}
	head := "Очереди"
	qs, _ := u.st.ListQueues()
	for _, q := range qs {
		if !q.Enabled {
			continue
		}
		for _, a := range q.AllAliases() {
			if add(a, "очередь", head, 0, domain.CatalogEntry{Provider: "Очередь"}) {
				head = ""
			}
		}
	}
	head = "Alias в шлюзе"
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
	byProv := map[string][]domain.CatalogEntry{}
	for _, e := range cat {
		if e.Name == "" || seen[e.Name] {
			continue
		}
		p := e.Provider
		if p == "" {
			p = "Другие"
		}
		byProv[p] = append(byProv[p], e)
	}
	provOrder := make([]string, 0, len(byProv))
	for p := range byProv {
		provOrder = append(provOrder, p)
	}
	sort.Strings(provOrder)
	for _, p := range provOrder {
		items := byProv[p]
		sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
		head := fmt.Sprintf("%s · %d", p, len(items))
		for _, e := range items {
			if add(e.Name, p, head, e.Context, e) {
				head = ""
			}
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
	u.keys.PutFlash(w, plain)
	http.Redirect(w, r, "/admin/keys?ok=key_created", http.StatusFound)
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

type logRow struct {
	domain.RequestLog
	Kind      string
	TimeLabel string
	TimeFull  string
	SizeLabel string
}

func (u *UI) logs(w http.ResponseWriter, r *http.Request) {
	ls, _ := u.st.ListLogs(500)
	rows := make([]logRow, 0, len(ls))
	var models, backends, keys []string
	for _, l := range ls {
		ts := l.TS.Local()
		label := ts.Format("15:04:05")
		if ts.Format("2006-01-02") != time.Now().Format("2006-01-02") {
			label = ts.Format("02.01 15:04:05")
		}
		rows = append(rows, logRow{
			RequestLog: l, Kind: logStatusKind(l.Status),
			TimeLabel: label, TimeFull: ts.Format("02.01.2006 15:04:05"),
			SizeLabel: humanSize(l.BytesOut),
		})
		models = append(models, l.Model)
		backends = append(backends, l.Backend)
		keys = append(keys, l.KeyPrefix)
	}
	u.render(w, r, "logs", map[string]any{
		"Title": "Лог", "Nav": "logs", "Logs": rows, "Q": r.URL.Query().Get("q"),
		"Models": uniqueSorted(models), "Backends": uniqueSorted(backends), "Keys": uniqueSorted(keys),
	})
}

func (u *UI) billing(w http.ResponseWriter, r *http.Request) {
	view, err := u.st.Billing(r.URL.Query().Get("p"), time.Now())
	if err != nil {
		u.render(w, r, "billing", map[string]any{
			"Title": "Биллинг", "Nav": "billing", "Error": err.Error(),
			"View": domain.BillingView{Period: "day"}, "Caption": "последние 30 дней",
			"Periods": []struct {
				ID, Label string
				Active    bool
			}{{ID: "day", Label: "День", Active: true}},
		})
		return
	}
	type pill struct {
		ID, Label string
		Active    bool
	}
	periods := []pill{
		{ID: "hour", Label: "Час"},
		{ID: "day", Label: "День"},
		{ID: "week", Label: "Неделя"},
		{ID: "month", Label: "Месяц"},
		{ID: "year", Label: "Год"},
	}
	for i := range periods {
		periods[i].Active = periods[i].ID == view.Period
	}
	caption := "последние 30 дней"
	switch view.Period {
	case "hour":
		caption = "последние 24 часа"
	case "week":
		caption = "последние 12 недель"
	case "month":
		caption = "последние 12 месяцев"
	case "year":
		caption = "последние 5 лет"
	}
	u.render(w, r, "billing", map[string]any{
		"Title": "Биллинг", "Nav": "billing", "View": view, "Periods": periods, "Caption": caption,
	})
}

func logStatusKind(status int) string {
	switch {
	case status >= 200 && status < 300:
		return "2"
	case status >= 400 && status < 500:
		return "4"
	case status >= 500:
		return "5"
	default:
		return "0"
	}
}

func uniqueSorted(ss []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range ss {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
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
	case "catalog_refreshed":
		return "Каталог провайдера обновлён с сервера (включая video, если API отдаёт)."
	case "backend_saved":
		return "Сервер добавлен. Обновите модели, если список пустой."
	case "backend_enabled":
		return "Провайдер включён. Шлюз снова будет на него ходить."
	case "backend_disabled":
		return "Провайдер выключен. Health и маршруты его пропускают."
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
	case "key_created":
		return "Ключ создан. Скопируйте его сейчас."
	case "key_updated":
		return "Доступ ключа обновлён."
	case "context_saved":
		return "Контекст модели сохранён."
	case "fallback_saved":
		return "Запасная модель сохранена."
	case "prompt_cache_saved":
		return "Настройка prompt cache сохранена."
	case "hub_saved":
		return "Настройка hub сохранена. Клиент сам зарегистрируется на хабе."
	case "hub_share_saved":
		return "Публикация alias в hub обновлена."
	case "queue_saved":
		return "Очередь сохранена."
	case "queue_deleted":
		return "Очередь удалена."
	case "mcp_token":
		return "MCP-токен выпущен. Скопируйте его сейчас."
	default:
		return ""
	}
}

func errMsg(code string) string {
	switch code {
	case "reserved_auto":
		return "Alias auto задаёт оператор хаба. Его нельзя создать, шарить или удалить вручную."
	case "select_models":
		return "Выберите хотя бы одну модель."
	case "no_backend":
		return "Нет живого сервера с этой моделью. Нажмите «Обновить каталоги». Для OpenComfy: тип OpenComfy, URL без /v1, ключ sk-…, карточка online со списком моделей, затем «В шлюз»."
	case "token+required":
		return "Для OpenRouter, Ollama Cloud и OpenComfy нужен API-ключ."
	case "name+and+url+required":
		return "Нужны имя и URL."
	case "min+6+chars", "min+8+chars":
		return "Пароль не короче 8 символов."
	default:
		if len(code) > 200 {
			code = code[:200]
		}
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

func (u *UI) render(w http.ResponseWriter, r *http.Request, name string, data map[string]any) {
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
	if _, ok := data["CSRF"]; !ok && u.keys != nil {
		data["CSRF"] = u.keys.CSRF(r)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := t.ExecuteTemplate(w, "layout.html", data); err != nil {
		http.Error(w, err.Error(), 500)
	}
}
