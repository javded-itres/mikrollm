package mcp

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/javded-itres/mikrollm/internal/auth"
	"github.com/javded-itres/mikrollm/internal/domain"
	"github.com/javded-itres/mikrollm/internal/ports"
)

func objSchema(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func (s *Server) buildTools() []toolDef {
	str := map[string]any{"type": "string"}
	num := map[string]any{"type": "integer"}
	bol := map[string]any{"type": "boolean"}
	return []toolDef{
		{Name: "get_status", Description: "Сводка шлюза: версия, RAM, бэкенды (health/latency/модели в RAM), alias, живые очереди, фоновые jobs.", Schema: objSchema(nil), Fn: s.toolStatus},
		{Name: "refresh_health", Description: "Внеочередной опрос всех бэкендов, затем та же сводка, что get_status.", Schema: objSchema(nil), Fn: s.toolRefresh},
		{Name: "list_providers", Description: "Список провайдеров/бэкендов (Ollama, vLLM, LM Studio, OpenRouter, Ollama Cloud). Токен маскируется.", Schema: objSchema(nil), Fn: s.toolListProviders},
		{Name: "upsert_provider", Description: "Добавить или обновить бэкенд. Идентичность — base_url. Пустой token не затирает уже сохранённый ключ. Для правки передайте id или url.", Schema: objSchema(map[string]any{
			"name": str, "base_url": str, "kind": map[string]any{"type": "string", "description": "ollama | ollama-cloud | openrouter | vllm | lmstudio"},
			"token": str, "enabled": bol, "weight": num, "id": num,
		}), Fn: s.toolUpsertProvider},
		{Name: "delete_provider", Description: "Удалить бэкенд по id.", Schema: objSchema(map[string]any{"id": num}, "id"), Fn: s.toolDeleteProvider},
		{Name: "list_models", Description: "Alias шлюза: upstream, бэкенды, LB, контекст, fallback.", Schema: objSchema(nil), Fn: s.toolListModels},
		{Name: "list_catalog", Description: "Модели, которые health видит на бэкендах. Фильтры q/provider, лимит.", Schema: objSchema(map[string]any{
			"q": str, "provider": str, "limit": num, "unconnected_only": bol,
		}), Fn: s.toolCatalog},
		{Name: "connect_model", Description: "Подключить имя из каталога как alias шлюза (как «В шлюз» в админке).", Schema: objSchema(map[string]any{
			"name": str, "names": map[string]any{"type": "array", "items": str},
			"lb_policy": map[string]any{"type": "string", "description": "least_conn | round_robin | failover"},
		}), Fn: s.toolConnectModel},
		{Name: "save_model", Description: "Создать или обновить alias. Если backend_ids пусты — берёт их из каталога.", Schema: objSchema(map[string]any{
			"id": num, "alias": str, "upstream_name": str,
			"backend_ids": map[string]any{"type": "array", "items": num},
			"lb_policy":   str, "max_context": num, "fallback": str, "enabled": bol,
		}, "alias"), Fn: s.toolSaveModel},
		{Name: "delete_model", Description: "Удалить alias по id или имени.", Schema: objSchema(map[string]any{"id": num, "alias": str}), Fn: s.toolDeleteModel},
		{Name: "host_action", Description: "Операция на хосте: pull/load (фон) или unload/delete. vLLM/облако часть действий не умеют.", Schema: objSchema(map[string]any{
			"action":     map[string]any{"type": "string", "description": "pull | load | unload | delete"},
			"backend_id": num, "model": str,
		}, "action", "backend_id", "model"), Fn: s.toolHostAction},
		{Name: "list_jobs", Description: "Фоновые pull/load: статус, процент, ошибка.", Schema: objSchema(nil), Fn: s.toolListJobs},
		{Name: "list_queues", Description: "Очереди: шаги, слоты, ждущие/идущие, лента jobs, extra alias.", Schema: objSchema(nil), Fn: s.toolListQueues},
		{Name: "save_queue", Description: "Создать/обновить очередь. steps и extra_aliases, если переданы, заменяют текущие.", Schema: objSchema(map[string]any{
			"id": num, "name": str, "alias": str, "enabled": bol,
			"overflow_after": num, "overflow_alias": str, "max_wait_ms": num,
			"steps": map[string]any{"type": "array", "items": objSchema(map[string]any{
				"model_alias": str, "max_concurrent": num,
			}, "model_alias")},
			"extra_aliases": map[string]any{"type": "array", "items": str},
		}, "name", "alias"), Fn: s.toolSaveQueue},
		{Name: "delete_queue", Description: "Удалить очередь по id или alias.", Schema: objSchema(map[string]any{"id": num, "alias": str}), Fn: s.toolDeleteQueue},
		{Name: "list_keys", Description: "Виртуальные ключи (prefix, allowlist, RPM). Секрет не возвращается.", Schema: objSchema(nil), Fn: s.toolListKeys},
		{Name: "create_key", Description: "Выпустить sk- ключ. models пустой или [\"*\"] — все модели. Секрет показывается один раз.", Schema: objSchema(map[string]any{
			"name": str, "rpm": num, "all_models": bol,
			"models": map[string]any{"type": "array", "items": str},
		}), Fn: s.toolCreateKey},
		{Name: "update_key", Description: "Изменить имя, RPM, enabled, allowlist. Секрет не меняется.", Schema: objSchema(map[string]any{
			"id": num, "name": str, "rpm": num, "enabled": bol, "all_models": bol,
			"models": map[string]any{"type": "array", "items": str},
		}, "id"), Fn: s.toolUpdateKey},
		{Name: "delete_key", Description: "Отозвать ключ по id.", Schema: objSchema(map[string]any{"id": num}, "id"), Fn: s.toolDeleteKey},
		{Name: "list_logs", Description: "Последние запросы шлюза. Фильтры: q, model, backend, key, status/status_class (2xx/4xx/5xx), latency.", Schema: objSchema(map[string]any{
			"limit": num, "q": str, "model": str, "backend": str, "key": str,
			"status": num, "status_class": map[string]any{"type": "string", "description": "2xx | 4xx | 5xx"},
			"min_latency_ms": num, "max_latency_ms": num,
		}), Fn: s.toolListLogs},
		{Name: "log_stats", Description: "Агрегаты по последним логам: счётчики по статусу/модели/бэкенду, p50/p95 latency, error rate.", Schema: objSchema(map[string]any{"limit": num}), Fn: s.toolLogStats},
		{Name: "rotate_mcp_token", Description: "Выпустить новый MCP Bearer-токен. Старый сразу недействителен. Новый показывается один раз.", Schema: objSchema(nil), Fn: s.toolRotateMCP},
	}
}

func (s *Server) toolStatus(map[string]any) (any, error) {
	return s.statusPayload(), nil
}

func (s *Server) toolRefresh(map[string]any) (any, error) {
	if s.health != nil {
		s.health.CheckOnce()
	}
	return s.statusPayload(), nil
}

func (s *Server) statusPayload() map[string]any {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	bs, _ := s.st.ListBackends()
	aliases, _ := s.st.ListModels()
	var backends []map[string]any
	up := 0
	for _, b := range bs {
		st := domain.HostStatus{}
		if s.health != nil {
			st = s.health.Get(b.ID)
		}
		if st.Healthy {
			up++
		}
		lat := int64(0)
		if st.Latency > 0 {
			lat = st.Latency.Milliseconds()
		}
		backends = append(backends, map[string]any{
			"id": b.ID, "name": b.Name, "kind": b.KindNorm(), "label": b.Label(),
			"url": b.BaseURL, "enabled": b.Enabled, "healthy": st.Healthy,
			"latency_ms": lat, "error": st.Error, "models": len(st.Models), "running": st.Running,
			"has_token": b.Token != "",
		})
	}
	var qv []domain.QueueView
	waitN := 0
	if s.queues != nil {
		qv = s.queues.Snapshot()
		for _, q := range qv {
			waitN += q.Waiting + q.Running
		}
	}
	var jobs []domain.Job
	running := false
	if s.jobs != nil {
		jobs = s.jobs.List()
		running = s.jobs.Running()
	}
	prefix, _ := s.st.MCPTokenPrefix()
	return map[string]any{
		"version": s.version,
		"runtime": map[string]any{
			"go": runtime.Version(), "goroutines": runtime.NumGoroutine(),
			"alloc_bytes": ms.Alloc, "sys_bytes": ms.Sys,
		},
		"backends_up": up, "backends_total": len(bs), "aliases": len(aliases),
		"queue_live": waitN, "mcp_prefix": prefix,
		"backends": backends, "queues": qv, "jobs": jobs, "jobs_running": running,
	}
}

func (s *Server) toolListProviders(map[string]any) (any, error) {
	bs, err := s.st.ListBackends()
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	for _, b := range bs {
		st := domain.HostStatus{}
		if s.health != nil {
			st = s.health.Get(b.ID)
		}
		item := map[string]any{
			"id": b.ID, "name": b.Name, "kind": b.KindNorm(), "label": b.Label(),
			"url": b.BaseURL, "enabled": b.Enabled, "weight": b.Weight,
			"healthy": st.Healthy, "error": st.Error, "models": st.Models, "running": st.Running,
			"cloud": b.Cloud(), "can_pull": b.CanPull(), "can_load": b.CanLoad(), "can_delete": b.CanDelete(),
			"has_token": b.Token != "", "token_hint": maskToken(b.Token),
		}
		if !st.Checked.IsZero() {
			item["checked"] = st.Checked.UTC().Format(time.RFC3339)
			item["latency_ms"] = st.Latency.Milliseconds()
		}
		out = append(out, item)
	}
	return map[string]any{"providers": out}, nil
}

func (s *Server) toolUpsertProvider(args map[string]any) (any, error) {
	name := strArg(args, "name")
	kind := strArg(args, "kind")
	rawURL := strArg(args, "base_url", "url")
	token := strArg(args, "token")
	if id, ok := intArg(args, "id"); ok && id > 0 {
		old, err := s.st.GetBackend(id)
		if err == nil {
			if name == "" {
				name = old.Name
			}
			if kind == "" {
				kind = old.Kind
			}
			if rawURL == "" {
				rawURL = old.BaseURL
			}
			if token == "" {
				token = old.Token
			}
		}
	}
	url, err := domain.SanitizeBackendURL(kind, rawURL)
	if name == "" || err != nil || url == "" {
		return nil, fmt.Errorf("нужны name и корректный http(s) URL")
	}
	if domain.RequiresToken(kind) && token == "" {
		return nil, fmt.Errorf("для OpenRouter и Ollama Cloud нужен API-ключ")
	}
	enabled := boolArg(args, "enabled", true)
	weight := 1
	if n, ok := intArg(args, "weight"); ok && n > 0 {
		weight = int(n)
	}
	id, err := s.st.UpsertBackend(name, url, enabled, weight, kind, token)
	if err != nil {
		return nil, err
	}
	if s.health != nil {
		go s.health.CheckOnce()
	}
	return map[string]any{"ok": true, "id": id, "name": name, "url": url, "kind": domain.NormalizeKind(kind)}, nil
}

func (s *Server) toolDeleteProvider(args map[string]any) (any, error) {
	id, ok := intArg(args, "id")
	if !ok || id <= 0 {
		return nil, fmt.Errorf("id required")
	}
	if err := s.st.DeleteBackend(id); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "id": id}, nil
}

func (s *Server) toolListModels(map[string]any) (any, error) {
	ms, err := s.st.ListModels()
	if err != nil {
		return nil, err
	}
	bs, _ := s.st.ListBackends()
	bmap := map[int64]domain.Backend{}
	for _, b := range bs {
		bmap[b.ID] = b
	}
	cat := []domain.CatalogEntry{}
	if s.health != nil {
		cat = s.health.Catalog(bs)
	}
	byName := map[string]domain.CatalogEntry{}
	for _, e := range cat {
		byName[e.Name] = e
	}
	var out []map[string]any
	for _, m := range ms {
		var names []string
		for _, id := range m.BackendIDs {
			names = append(names, bmap[id].Name)
		}
		meta := byName[m.UpstreamName]
		if meta.Name == "" {
			meta = byName[m.Alias]
		}
		out = append(out, map[string]any{
			"id": m.ID, "alias": m.Alias, "upstream_name": m.UpstreamName,
			"lb_policy": m.LBPolicy, "enabled": m.Enabled, "backend_ids": m.BackendIDs,
			"backends": names, "max_context": m.MaxContext, "fallback": m.Fallback,
			"context": m.ContextWindow(meta.Context), "provider": meta.Provider,
			"priced": meta.Priced, "prompt_usd": meta.PromptUSD, "completion_usd": meta.CompletionUSD,
		})
	}
	return map[string]any{"models": out}, nil
}

func (s *Server) toolCatalog(args map[string]any) (any, error) {
	bs, err := s.st.ListBackends()
	if err != nil {
		return nil, err
	}
	if s.health == nil {
		return map[string]any{"catalog": []any{}}, nil
	}
	q := strings.ToLower(strArg(args, "q", "query"))
	prov := strings.ToLower(strArg(args, "provider"))
	onlyNew := boolArg(args, "unconnected_only", false)
	limit := 80
	if n, ok := intArg(args, "limit"); ok {
		limit = int(n)
	}
	if limit <= 0 {
		limit = 80
	}
	if limit > 300 {
		limit = 300
	}
	connected := map[string]bool{}
	ms, _ := s.st.ListModels()
	for _, m := range ms {
		connected[m.Alias] = true
		if m.UpstreamName != "" {
			connected[m.UpstreamName] = true
		}
	}
	var out []map[string]any
	for _, e := range s.health.Catalog(bs) {
		if onlyNew && connected[e.Name] {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(e.Name+" "+e.Title+" "+e.Provider), q) {
			continue
		}
		if prov != "" && !strings.Contains(strings.ToLower(e.Provider), prov) {
			continue
		}
		out = append(out, map[string]any{
			"name": e.Name, "title": e.Title, "provider": e.Provider,
			"backend_ids": e.BackendIDs, "backends": e.BackendNames,
			"size": e.Size, "loaded_on": e.LoadedOn, "context": e.Context,
			"priced": e.Priced, "prompt_usd": e.PromptUSD, "completion_usd": e.CompletionUSD,
			"connected": connected[e.Name],
		})
		if len(out) >= limit {
			break
		}
	}
	return map[string]any{"catalog": out, "count": len(out)}, nil
}

func (s *Server) toolConnectModel(args map[string]any) (any, error) {
	names, _ := strSlice(args, "names")
	if n := strArg(args, "name", "model"); n != "" {
		names = append(names, n)
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("name or names required")
	}
	policy := strArg(args, "lb_policy")
	bs, _ := s.st.ListBackends()
	cat := []domain.CatalogEntry{}
	if s.health != nil {
		cat = s.health.Catalog(bs)
	}
	byName := map[string]domain.CatalogEntry{}
	for _, e := range cat {
		byName[e.Name] = e
	}
	connected := 0
	var missing []string
	for _, name := range names {
		e, ok := byName[name]
		if !ok || len(e.BackendIDs) == 0 {
			missing = append(missing, name)
			continue
		}
		if err := s.st.ConnectOllamaModel(name, e.BackendIDs, policy, e.Context); err != nil {
			return nil, err
		}
		connected++
	}
	if connected == 0 {
		return nil, fmt.Errorf("модели не найдены в каталоге: %s", strings.Join(missing, ", "))
	}
	return map[string]any{"ok": true, "connected": connected, "missing": missing}, nil
}

func (s *Server) toolSaveModel(args map[string]any) (any, error) {
	alias := strArg(args, "alias")
	upstream := strArg(args, "upstream_name", "upstream")
	if alias == "" {
		alias = upstream
	}
	if alias == "" {
		return nil, fmt.Errorf("alias required")
	}
	m := domain.Model{Alias: alias, UpstreamName: upstream, LBPolicy: strArg(args, "lb_policy"), Enabled: true, Fallback: strArg(args, "fallback")}
	if n, ok := intArg(args, "max_context"); ok {
		m.MaxContext = int(n)
	}
	if ids, ok := intSlice(args, "backend_ids"); ok {
		m.BackendIDs = ids
	}
	if hasArg(args, "enabled") {
		m.Enabled = boolArg(args, "enabled", true)
	}
	if id, ok := intArg(args, "id"); ok && id > 0 {
		if old, err := s.st.GetModel(id); err == nil {
			m.ID = old.ID
			if !hasArg(args, "enabled") {
				m.Enabled = old.Enabled
			}
			if m.LBPolicy == "" {
				m.LBPolicy = old.LBPolicy
			}
			if m.UpstreamName == "" {
				m.UpstreamName = old.UpstreamName
			}
			if !hasArg(args, "max_context") {
				m.MaxContext = old.MaxContext
			}
			if !hasArg(args, "fallback") {
				m.Fallback = old.Fallback
			}
			if !hasArg(args, "backend_ids") {
				m.BackendIDs = old.BackendIDs
			}
		}
	} else if old, err := s.st.GetModelByAlias(alias); err == nil {
		m.ID = old.ID
		if !hasArg(args, "backend_ids") && len(m.BackendIDs) == 0 {
			m.BackendIDs = old.BackendIDs
		}
		if m.UpstreamName == "" {
			m.UpstreamName = old.UpstreamName
		}
		if !hasArg(args, "max_context") {
			m.MaxContext = old.MaxContext
		}
		if !hasArg(args, "fallback") {
			m.Fallback = old.Fallback
		}
		if m.LBPolicy == "" {
			m.LBPolicy = old.LBPolicy
		}
	}
	if len(m.BackendIDs) == 0 {
		bs, _ := s.st.ListBackends()
		if s.health != nil {
			for _, e := range s.health.Catalog(bs) {
				if e.Name == m.UpstreamName || e.Name == m.Alias {
					m.BackendIDs = e.BackendIDs
					if m.UpstreamName == "" {
						m.UpstreamName = e.Name
					}
					break
				}
			}
		}
	}
	if len(m.BackendIDs) == 0 {
		return nil, fmt.Errorf("нет бэкенда с этой моделью — вызовите refresh_health или укажите backend_ids")
	}
	id, err := s.st.SaveModel(m)
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "id": id, "alias": m.Alias}, nil
}

func (s *Server) toolDeleteModel(args map[string]any) (any, error) {
	id, ok := intArg(args, "id")
	if !ok || id <= 0 {
		alias := strArg(args, "alias", "name")
		if alias == "" {
			return nil, fmt.Errorf("id or alias required")
		}
		m, err := s.st.GetModelByAlias(alias)
		if err != nil {
			return nil, fmt.Errorf("модель %s не найдена", alias)
		}
		id = m.ID
	}
	if err := s.st.DeleteModel(id); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "id": id}, nil
}

func (s *Server) toolHostAction(args map[string]any) (any, error) {
	op := strings.ToLower(strArg(args, "action", "op"))
	model := strArg(args, "model", "name")
	bid, ok := intArg(args, "backend_id", "id")
	if !ok || bid <= 0 || model == "" {
		return nil, fmt.Errorf("action, backend_id и model обязательны")
	}
	b, err := s.st.GetBackend(bid)
	if err != nil || !b.Enabled {
		return nil, fmt.Errorf("бэкенд %d недоступен", bid)
	}
	if msg := rejectHostOp(b, op); msg != "" {
		return nil, fmt.Errorf("%s", msg)
	}
	if s.host == nil {
		return nil, fmt.Errorf("host adapter unavailable")
	}
	switch op {
	case "unload":
		if err := s.host.Unload(context.Background(), b, model); err != nil {
			return nil, err
		}
		if s.health != nil {
			s.health.CheckOnce()
		}
		return map[string]any{"ok": true, "status": "unloaded"}, nil
	case "delete":
		if err := s.host.Delete(context.Background(), b, model); err != nil {
			return nil, err
		}
		if s.health != nil {
			s.health.CheckOnce()
		}
		return map[string]any{"ok": true, "status": "deleted"}, nil
	case "pull", "load":
		if s.jobs == nil {
			return nil, fmt.Errorf("jobs tracker unavailable")
		}
		j, busy, err := s.jobs.Begin(op, bid, b.Name, model)
		if err != nil {
			msg := "busy"
			if busy != nil {
				msg = busy.Kind + " " + busy.Model
			}
			if err != ports.ErrBusy {
				msg = err.Error()
			}
			return nil, fmt.Errorf("%s", msg)
		}
		go func(j domain.Job, b domain.Backend, model, op string) {
			var runErr error
			if op == "pull" {
				wr := s.jobs.Writer(j.ID)
				runErr = s.host.Pull(context.Background(), b, model, wr)
				_ = wr.Close()
			} else {
				s.jobs.SetMessage(j.ID, "загрузка в RAM…")
				runErr = s.host.Load(context.Background(), b, model)
			}
			if runErr != nil {
				s.jobs.Fail(j.ID, runErr.Error())
				return
			}
			s.jobs.Done(j.ID)
			if s.health != nil {
				s.health.CheckOnce()
			}
		}(*j, b, model, op)
		return map[string]any{"ok": true, "status": "started", "job_id": j.ID, "job": j}, nil
	default:
		return nil, fmt.Errorf("action: pull | load | unload | delete")
	}
}

func rejectHostOp(b domain.Backend, op string) string {
	ok := false
	switch op {
	case "pull":
		ok = b.CanPull()
	case "load", "unload":
		ok = b.CanLoad()
	case "delete":
		ok = b.CanDelete()
	}
	if ok {
		return ""
	}
	if h := b.LoadHint(); h != "" {
		return h
	}
	return b.Label() + " не поддерживает эту операцию через API"
}

func (s *Server) toolListJobs(map[string]any) (any, error) {
	if s.jobs == nil {
		return map[string]any{"jobs": []any{}, "running": false}, nil
	}
	return map[string]any{"jobs": s.jobs.List(), "running": s.jobs.Running()}, nil
}

func (s *Server) toolListQueues(map[string]any) (any, error) {
	qs, err := s.st.ListQueues()
	if err != nil {
		return nil, err
	}
	var live []domain.QueueView
	if s.queues != nil {
		live = s.queues.Snapshot()
	}
	byID := map[int64]domain.QueueView{}
	for _, v := range live {
		byID[v.ID] = v
	}
	var out []map[string]any
	for _, q := range qs {
		item := map[string]any{
			"id": q.ID, "name": q.Name, "alias": q.Alias, "enabled": q.Enabled,
			"overflow_after": q.OverflowAfter, "overflow_alias": q.OverflowAlias,
			"max_wait_ms": q.MaxWaitMS, "steps": q.Steps, "extra_aliases": q.ExtraAliases,
			"all_aliases": q.AllAliases(),
		}
		if v, ok := byID[q.ID]; ok {
			item["waiting"] = v.Waiting
			item["running"] = v.Running
			item["bytes"] = v.Bytes
			item["live_steps"] = v.Steps
			item["jobs"] = v.Jobs
		}
		out = append(out, item)
	}
	return map[string]any{"queues": out}, nil
}

func (s *Server) toolSaveQueue(args map[string]any) (any, error) {
	q := domain.Queue{
		Name: strArg(args, "name"), Alias: strArg(args, "alias"), Enabled: true,
		OverflowAlias: strArg(args, "overflow_alias"),
	}
	if id, ok := intArg(args, "id"); ok && id > 0 {
		old, err := s.st.GetQueue(id)
		if err != nil {
			return nil, err
		}
		q = old
		if hasArg(args, "name") {
			q.Name = strArg(args, "name")
		}
		if hasArg(args, "alias") {
			q.Alias = strArg(args, "alias")
		}
		if hasArg(args, "overflow_alias") {
			q.OverflowAlias = strArg(args, "overflow_alias")
		}
	}
	if hasArg(args, "enabled") {
		q.Enabled = boolArg(args, "enabled", true)
	}
	if n, ok := intArg(args, "overflow_after"); ok {
		q.OverflowAfter = int(n)
	} else if q.ID == 0 {
		q.OverflowAfter = 5
	}
	if n, ok := intArg(args, "max_wait_ms"); ok {
		q.MaxWaitMS = int(n)
	}
	if raw, ok := args["steps"].([]any); ok {
		var steps []domain.QueueStep
		for _, x := range raw {
			m, _ := x.(map[string]any)
			if m == nil {
				continue
			}
			alias := stringify(m["model_alias"])
			if alias == "" {
				alias = stringify(m["alias"])
			}
			if alias == "" {
				continue
			}
			capn := 1
			if n, ok := asInt(m["max_concurrent"]); ok && n > 0 {
				capn = int(n)
			}
			steps = append(steps, domain.QueueStep{ModelAlias: alias, MaxConcurrent: capn})
		}
		q.Steps = steps
	}
	id, err := s.st.SaveQueue(q)
	if err != nil {
		return nil, err
	}
	if aliases, ok := strSlice(args, "extra_aliases"); ok {
		if err := s.replaceQueueAliases(id, aliases); err != nil {
			return nil, err
		}
	}
	saved, _ := s.st.GetQueue(id)
	return map[string]any{"ok": true, "queue": saved}, nil
}

func asInt(v any) (int64, bool) {
	return intArg(map[string]any{"n": v}, "n")
}

func (s *Server) replaceQueueAliases(id int64, want []string) error {
	q, err := s.st.GetQueue(id)
	if err != nil {
		return err
	}
	keep := map[string]bool{}
	for _, a := range want {
		a = strings.TrimSpace(a)
		if a == "" || a == q.Alias {
			continue
		}
		keep[a] = true
		found := false
		for _, e := range q.ExtraAliases {
			if e == a {
				found = true
				break
			}
		}
		if !found {
			if err := s.st.AddQueueAlias(id, a); err != nil {
				return err
			}
		}
	}
	for _, e := range q.ExtraAliases {
		if !keep[e] {
			_ = s.st.DeleteQueueAlias(id, e)
		}
	}
	return nil
}

func (s *Server) toolDeleteQueue(args map[string]any) (any, error) {
	id, ok := intArg(args, "id")
	if !ok || id <= 0 {
		alias := strArg(args, "alias", "name")
		if alias == "" {
			return nil, fmt.Errorf("id or alias required")
		}
		q, err := s.st.GetQueueByAlias(alias)
		if err != nil {
			qs, _ := s.st.ListQueues()
			for i := range qs {
				if qs[i].Name == alias {
					q, err = qs[i], nil
					break
				}
			}
		}
		if err != nil {
			return nil, fmt.Errorf("очередь %s не найдена", alias)
		}
		id = q.ID
	}
	if err := s.st.DeleteQueue(id); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "id": id}, nil
}

func (s *Server) toolListKeys(map[string]any) (any, error) {
	ks, err := s.st.ListKeys()
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	for _, k := range ks {
		item := map[string]any{
			"id": k.ID, "name": k.Name, "prefix": k.Prefix, "allowed_models": k.AllowedModels,
			"rpm": k.RPM, "enabled": k.Enabled, "request_count": k.RequestCount,
			"created_at": k.CreatedAt.UTC().Format(time.RFC3339),
		}
		if k.LastUsedAt != nil && !k.LastUsedAt.IsZero() {
			item["last_used_at"] = k.LastUsedAt.UTC().Format(time.RFC3339)
		}
		out = append(out, item)
	}
	return map[string]any{"keys": out}, nil
}

func keyModels(args map[string]any) ([]string, error) {
	if boolArg(args, "all_models", false) {
		return []string{"*"}, nil
	}
	models, ok := strSlice(args, "models")
	if !ok {
		if s := strArg(args, "model"); s != "" {
			models = []string{s}
			ok = true
		}
	}
	if !ok || len(models) == 0 {
		return nil, fmt.Errorf("укажите models или all_models=true")
	}
	return models, nil
}

func (s *Server) toolCreateKey(args map[string]any) (any, error) {
	models, err := keyModels(args)
	if err != nil {
		return nil, err
	}
	plain, prefix, hash, err := auth.GenerateKey()
	if err != nil {
		return nil, err
	}
	rpm := 0
	if n, ok := intArg(args, "rpm"); ok {
		rpm = int(n)
	}
	name := strArg(args, "name")
	if name == "" {
		name = "key"
	}
	k := domain.APIKey{Name: name, Prefix: prefix, KeyHash: hash, AllowedModels: models, RPM: rpm, Enabled: true}
	id, err := s.st.InsertKey(k)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"ok": true, "id": id, "name": name, "prefix": prefix,
		"key": plain, "allowed_models": models, "rpm": rpm,
		"warning": "скопируйте key сейчас — повторно не покажем",
	}, nil
}

func (s *Server) toolUpdateKey(args map[string]any) (any, error) {
	id, ok := intArg(args, "id")
	if !ok || id <= 0 {
		return nil, fmt.Errorf("id required")
	}
	k, err := s.st.GetKey(id)
	if err != nil {
		return nil, fmt.Errorf("unknown key")
	}
	if name := strArg(args, "name"); name != "" {
		k.Name = name
	}
	if n, ok := intArg(args, "rpm"); ok {
		k.RPM = int(n)
	}
	if hasArg(args, "enabled") {
		k.Enabled = boolArg(args, "enabled", true)
	}
	if hasArg(args, "models") || hasArg(args, "all_models") || hasArg(args, "model") {
		models, err := keyModels(args)
		if err != nil {
			return nil, err
		}
		k.AllowedModels = models
	}
	if err := s.st.UpdateKey(k); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "id": k.ID, "name": k.Name, "allowed_models": k.AllowedModels, "rpm": k.RPM, "enabled": k.Enabled}, nil
}

func (s *Server) toolDeleteKey(args map[string]any) (any, error) {
	id, ok := intArg(args, "id")
	if !ok || id <= 0 {
		return nil, fmt.Errorf("id required")
	}
	if err := s.st.DeleteKey(id); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "id": id}, nil
}

func (s *Server) toolListLogs(args map[string]any) (any, error) {
	limit := 100
	if n, ok := intArg(args, "limit"); ok {
		limit = int(n)
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	ls, err := s.st.ListLogs(500)
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(strArg(args, "q", "query"))
	model := strArg(args, "model")
	backend := strArg(args, "backend")
	key := strArg(args, "key", "key_prefix")
	class := strings.ToLower(strArg(args, "status_class", "class"))
	var status int
	hasStatus := false
	if n, ok := intArg(args, "status"); ok {
		status, hasStatus = int(n), true
	}
	minLat, hasMin := intArg(args, "min_latency_ms")
	maxLat, hasMax := intArg(args, "max_latency_ms")
	var out []map[string]any
	for _, l := range ls {
		if model != "" && l.Model != model {
			continue
		}
		if backend != "" && l.Backend != backend {
			continue
		}
		if key != "" && l.KeyPrefix != key {
			continue
		}
		if hasStatus && l.Status != status {
			continue
		}
		if class != "" && logClass(l.Status) != class {
			continue
		}
		if hasMin && l.LatencyMS < minLat {
			continue
		}
		if hasMax && l.LatencyMS > maxLat {
			continue
		}
		if q != "" {
			blob := strings.ToLower(l.Model + " " + l.Backend + " " + l.KeyPrefix)
			if !strings.Contains(blob, q) {
				continue
			}
		}
		out = append(out, map[string]any{
			"id": l.ID, "ts": l.TS.UTC().Format(time.RFC3339Nano),
			"key": l.KeyPrefix, "model": l.Model, "backend": l.Backend,
			"status": l.Status, "latency_ms": l.LatencyMS, "bytes_out": l.BytesOut,
		})
		if len(out) >= limit {
			break
		}
	}
	return map[string]any{"logs": out, "count": len(out)}, nil
}

func logClass(status int) string {
	switch {
	case status >= 200 && status < 300:
		return "2xx"
	case status >= 400 && status < 500:
		return "4xx"
	case status >= 500:
		return "5xx"
	default:
		return "other"
	}
}

func (s *Server) toolLogStats(args map[string]any) (any, error) {
	limit := 500
	if n, ok := intArg(args, "limit"); ok && n > 0 && n <= 500 {
		limit = int(n)
	}
	ls, err := s.st.ListLogs(limit)
	if err != nil {
		return nil, err
	}
	byStatus := map[string]int{}
	byModel := map[string]int{}
	byBackend := map[string]int{}
	var lats []int
	errN := 0
	for _, l := range ls {
		byStatus[logClass(l.Status)]++
		if l.Model != "" {
			byModel[l.Model]++
		}
		if l.Backend != "" {
			byBackend[l.Backend]++
		}
		lats = append(lats, int(l.LatencyMS))
		if l.Status >= 400 {
			errN++
		}
	}
	sort.Ints(lats)
	p50, p95 := 0, 0
	if n := len(lats); n > 0 {
		p50 = lats[(n-1)*50/100]
		p95 = lats[(n-1)*95/100]
	}
	rate := 0.0
	if len(ls) > 0 {
		rate = float64(errN) / float64(len(ls))
	}
	return map[string]any{
		"n": len(ls), "errors": errN, "error_rate": rate,
		"p50_latency_ms": p50, "p95_latency_ms": p95,
		"by_status": byStatus, "by_model": topN(byModel, 15), "by_backend": topN(byBackend, 15),
	}, nil
}

func topN(m map[string]int, n int) []map[string]any {
	type kv struct {
		k string
		v int
	}
	var all []kv
	for k, v := range m {
		all = append(all, kv{k, v})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].v == all[j].v {
			return all[i].k < all[j].k
		}
		return all[i].v > all[j].v
	})
	if len(all) > n {
		all = all[:n]
	}
	out := make([]map[string]any, 0, len(all))
	for _, x := range all {
		out = append(out, map[string]any{"name": x.k, "n": x.v})
	}
	return out
}

func (s *Server) toolRotateMCP(map[string]any) (any, error) {
	plain, prefix, _, err := auth.GenerateMCPToken()
	if err != nil {
		return nil, err
	}
	if _, err := s.st.SetMCPToken(plain); err != nil {
		return nil, err
	}
	return map[string]any{
		"ok": true, "prefix": prefix, "token": plain,
		"warning": "скопируйте token сейчас — старый больше не работает",
	}, nil
}

func maskToken(t string) string {
	t = domain.SanitizeToken(t)
	if t == "" {
		return ""
	}
	if len(t) <= 4 {
		return "****"
	}
	return "…" + t[len(t)-4:]
}
