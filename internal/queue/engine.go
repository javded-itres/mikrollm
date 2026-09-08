package queue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/javded-itres/mikrollm/internal/domain"
	"github.com/javded-itres/mikrollm/internal/ports"
)

type Router interface {
	TryRoute(model string) (backend, provider, upstream string, err error)
	Forward(ctx context.Context, w http.ResponseWriter, k domain.APIKey, path string, body []byte, model string) (backend, provider string, status int, err error)
}

type Limits struct {
	MaxBytes int64
	MaxJobs  int
	MaxWait  time.Duration
}

func LimitsFromEnv() Limits {
	return Limits{
		MaxBytes: envInt64("MIKROLLM_QUEUE_MAX_BYTES", 16<<20),
		MaxJobs:  int(envInt64("MIKROLLM_QUEUE_MAX_JOBS", 200)),
		MaxWait:  envDuration("MIKROLLM_QUEUE_MAX_WAIT", 3*time.Minute),
	}
}

type Engine struct {
	st       ports.QueueRepo
	router   Router
	limits   Limits
	mu       sync.Mutex
	inflight map[string]int
	waiters  map[string]*waiter
}

type waiter struct {
	job    *domain.QueueJob
	body   []byte
	key    domain.APIKey
	w      http.ResponseWriter
	ctx    context.Context
	done   chan struct{}
	once   sync.Once
	cancel context.CancelFunc
}

func (wt *waiter) complete() {
	wt.once.Do(func() { close(wt.done) })
}

func New(st ports.QueueRepo, router Router, limits Limits) *Engine {
	if limits.MaxBytes <= 0 {
		limits.MaxBytes = 16 << 20
	}
	if limits.MaxJobs <= 0 {
		limits.MaxJobs = 200
	}
	if limits.MaxWait <= 0 {
		limits.MaxWait = 3 * time.Minute
	}
	e := &Engine{st: st, router: router, limits: limits, inflight: map[string]int{}, waiters: map[string]*waiter{}}
	if st != nil {
		_ = st.ResetStaleQueueJobs()
	}
	return e
}

func (e *Engine) Lookup(alias string) (domain.Queue, bool) {
	if e == nil || e.st == nil {
		return domain.Queue{}, false
	}
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return domain.Queue{}, false
	}
	q, err := e.st.GetQueueByAlias(alias)
	if err == nil && q.Enabled {
		return q, true
	}
	qs, err := e.st.ListQueues()
	if err != nil {
		return domain.Queue{}, false
	}
	for _, q := range qs {
		if !q.Enabled {
			continue
		}
		if alias == q.Name || alias == q.Name+"-"+q.Alias || alias == q.Name+"/"+q.Alias {
			return q, true
		}
	}
	return domain.Queue{}, false
}

func (e *Engine) Handle(ctx context.Context, w http.ResponseWriter, k domain.APIKey, path string, body []byte, alias string) {
	e.handle(ctx, w, k, path, body, alias, 0)
}

func (e *Engine) handle(ctx context.Context, w http.ResponseWriter, k domain.APIKey, path string, body []byte, alias string, depth int) {
	if e == nil || e.st == nil {
		writeJSON(w, 502, "queue unavailable")
		return
	}
	if int64(len(body)) > e.limits.MaxBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, "request too large for queue")
		return
	}
	q, err := e.st.GetQueueByAlias(alias)
	if err != nil || !q.Enabled {
		writeJSON(w, 404, "queue not found")
		return
	}
	if depth > 2 {
		writeJSON(w, http.StatusServiceUnavailable, "queue overflow loop")
		return
	}

	e.purgeIfNeeded()

	waiting, running := e.counts(q.ID)
	if q.OverflowAfter > 0 && waiting >= q.OverflowAfter && q.OverflowAlias != "" && q.OverflowAlias != alias {
		j := e.newJob(q, alias, k, path, body)
		j.Status = domain.QueueOverflow
		j.AssignedModel = q.OverflowAlias
		now := time.Now().UTC()
		j.StartedAt = &now
		_ = e.st.InsertQueueJob(j)
		if _, ok := e.Lookup(q.OverflowAlias); ok {
			e.handle(ctx, w, k, path, body, q.OverflowAlias, depth+1)
			fin := time.Now().UTC()
			j.FinishedAt = &fin
			j.Status = domain.QueueOverflow
			j.Body = nil
			_ = e.st.UpdateQueueJob(*j)
			return
		}
		backend, provider, status, ferr := e.router.Forward(ctx, w, k, path, body, q.OverflowAlias)
		fin := time.Now().UTC()
		j.FinishedAt = &fin
		j.AssignedBackend = backend
		j.Provider = provider
		j.Body = nil
		if ferr != nil && status == 0 {
			j.Status = domain.QueueError
			j.Error = ferr.Error()
		} else if status >= 400 {
			j.Status = domain.QueueError
		} else {
			j.Status = domain.QueueDone
		}
		_ = e.st.UpdateQueueJob(*j)
		return
	}

	if q.OverflowAfter == 0 && running+waiting >= e.limits.MaxJobs {
		writeJSON(w, http.StatusServiceUnavailable, "queue full")
		return
	}

	j := e.newJob(q, alias, k, path, body)
	j.Status = domain.QueueWaiting
	if err := e.st.InsertQueueJob(j); err != nil {
		writeJSON(w, 500, err.Error())
		return
	}

	wait := e.limits.MaxWait
	if q.MaxWaitMS > 0 {
		wait = time.Duration(q.MaxWaitMS) * time.Millisecond
	}
	wctx, cancel := context.WithTimeout(ctx, wait)
	wt := &waiter{job: j, body: body, key: k, w: w, ctx: wctx, done: make(chan struct{}), cancel: cancel}
	e.mu.Lock()
	e.waiters[j.ID] = wt
	e.mu.Unlock()

	e.dispatch(q.ID)

	select {
	case <-wt.done:
	case <-wctx.Done():
		e.mu.Lock()
		waiting := wt.job.Status == domain.QueueWaiting
		e.mu.Unlock()
		if waiting {
			e.dropWaiter(j.ID, "timeout")
			writeJSON(w, http.StatusGatewayTimeout, "queue wait timeout")
		} else {
			<-wt.done
		}
	}
	cancel()
}

func (e *Engine) dispatch(queueID int64) {
	q, err := e.st.GetQueue(queueID)
	if err != nil {
		return
	}
	for {
		e.mu.Lock()
		wts := e.waitingLocked(queueID)
		if len(wts) == 0 {
			e.mu.Unlock()
			return
		}
		wt := wts[0]
		snap := map[string]int{}
		for k, v := range e.inflight {
			snap[k] = v
		}
		e.mu.Unlock()

		model, backend, provider, capn, ok := e.pickStep(q, snap)
		if !ok {
			return
		}

		e.mu.Lock()
		if wt.job.Status != domain.QueueWaiting {
			e.mu.Unlock()
			continue
		}
		if e.inflight[model] >= capn {
			e.mu.Unlock()
			continue
		}
		e.inflight[model]++
		now := time.Now().UTC()
		wt.job.Status = domain.QueueRunning
		wt.job.AssignedModel = model
		wt.job.AssignedBackend = backend
		wt.job.Provider = provider
		wt.job.StartedAt = &now
		jcopy := *wt.job
		jcopy.Body = nil
		e.mu.Unlock()
		_ = e.st.UpdateQueueJob(jcopy)
		go e.run(wt)
	}
}

func (e *Engine) pickStep(q domain.Queue, inflight map[string]int) (model, backend, provider string, capn int, ok bool) {
	for _, st := range q.Steps {
		capn = st.MaxConcurrent
		if capn <= 0 {
			capn = 1
		}
		if inflight[st.ModelAlias] >= capn {
			continue
		}
		b, p, _, err := e.router.TryRoute(st.ModelAlias)
		if err != nil {
			continue
		}
		return st.ModelAlias, b, p, capn, true
	}
	return "", "", "", 0, false
}

func (e *Engine) waitingLocked(queueID int64) []*waiter {
	type pair struct {
		seq int64
		wt  *waiter
	}
	var list []pair
	for _, wt := range e.waiters {
		if wt.job.QueueID == queueID && wt.job.Status == domain.QueueWaiting {
			list = append(list, pair{wt.job.Seq, wt})
		}
	}
	for i := 0; i < len(list); i++ {
		for j := i + 1; j < len(list); j++ {
			if list[j].seq < list[i].seq {
				list[i], list[j] = list[j], list[i]
			}
		}
	}
	out := make([]*waiter, len(list))
	for i, p := range list {
		out[i] = p.wt
	}
	return out
}

func (e *Engine) run(wt *waiter) {
	defer e.finish(wt)
	track := &headTracker{ResponseWriter: wt.w}
	backend, provider, status, err := e.router.Forward(wt.ctx, track, wt.key, wt.job.Path, wt.body, wt.job.AssignedModel)
	now := time.Now().UTC()
	wt.job.FinishedAt = &now
	if backend != "" {
		wt.job.AssignedBackend = backend
	}
	if provider != "" {
		wt.job.Provider = provider
	}
	wt.job.Body = nil
	if err != nil && !track.wrote {
		wt.job.Status = domain.QueueError
		wt.job.Error = err.Error()
		writeJSON(wt.w, 502, err.Error())
		return
	}
	if status >= 400 {
		wt.job.Status = domain.QueueError
		if wt.job.Error == "" {
			wt.job.Error = http.StatusText(status)
		}
		return
	}
	wt.job.Status = domain.QueueDone
}

func (e *Engine) finish(wt *waiter) {
	e.mu.Lock()
	if wt.job.AssignedModel != "" && e.inflight[wt.job.AssignedModel] > 0 {
		e.inflight[wt.job.AssignedModel]--
	}
	delete(e.waiters, wt.job.ID)
	qid := wt.job.QueueID
	e.mu.Unlock()
	_ = e.st.UpdateQueueJob(*wt.job)
	wt.complete()
	e.dispatch(qid)
}

func (e *Engine) dropWaiter(id, reason string) {
	e.mu.Lock()
	wt, ok := e.waiters[id]
	if ok {
		delete(e.waiters, id)
	}
	e.mu.Unlock()
	if !ok {
		return
	}
	now := time.Now().UTC()
	wt.job.Status = domain.QueueDropped
	wt.job.Error = reason
	wt.job.FinishedAt = &now
	wt.job.Body = nil
	_ = e.st.UpdateQueueJob(*wt.job)
	wt.complete()
}

func (e *Engine) counts(queueID int64) (waiting, running int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, wt := range e.waiters {
		if wt.job.QueueID != queueID {
			continue
		}
		switch wt.job.Status {
		case domain.QueueWaiting:
			waiting++
		case domain.QueueRunning:
			running++
		}
	}
	return
}

func (e *Engine) purgeIfNeeded() {
	jobs, bytes, err := e.st.WaitingStats()
	if err != nil {
		return
	}
	over := (e.limits.MaxJobs > 0 && jobs >= e.limits.MaxJobs) || (e.limits.MaxBytes > 0 && bytes >= e.limits.MaxBytes)
	if !over {
		_ = e.st.PurgeFinishedQueueJobs(40, 2*time.Hour)
		return
	}
	oldest, err := e.st.OldestWaiting(8)
	if err != nil {
		return
	}
	for _, j := range oldest {
		e.dropWaiter(j.ID, "queue size limit")
		jobs--
		bytes -= int64(j.Bytes)
		if (e.limits.MaxJobs <= 0 || jobs < e.limits.MaxJobs) && (e.limits.MaxBytes <= 0 || bytes < e.limits.MaxBytes) {
			break
		}
	}
}

func (e *Engine) Snapshot() []domain.QueueView {
	if e == nil || e.st == nil {
		return nil
	}
	qs, err := e.st.ListQueues()
	if err != nil {
		return nil
	}
	e.mu.Lock()
	inflight := map[string]int{}
	for k, v := range e.inflight {
		inflight[k] = v
	}
	e.mu.Unlock()
	var out []domain.QueueView
	for _, q := range qs {
		view := domain.QueueView{
			ID: q.ID, Name: q.Name, Alias: q.Alias,
			OverflowAfter: q.OverflowAfter, OverflowAlias: q.OverflowAlias,
			ExtraAliases: q.ExtraAliases,
		}
		for _, st := range q.Steps {
			capn := st.MaxConcurrent
			if capn <= 0 {
				capn = 1
			}
			_, provider, _, err := e.router.TryRoute(st.ModelAlias)
			busy := inflight[st.ModelAlias]
			pct := 0
			if capn > 0 {
				pct = busy * 100 / capn
				if pct > 100 {
					pct = 100
				}
			}
			sv := domain.QueueStepView{Alias: st.ModelAlias, Provider: provider, Busy: busy, Cap: capn, Percent: pct, Healthy: err == nil}
			view.Steps = append(view.Steps, sv)
		}
		jobs, err := e.st.ListQueueJobs(q.ID, 24)
		if err == nil {
			now := time.Now()
			for _, j := range jobs {
				if j.Status == domain.QueueWaiting {
					view.Waiting++
					view.Bytes += int64(j.Bytes)
				}
				if j.Status == domain.QueueRunning {
					view.Running++
					view.Bytes += int64(j.Bytes)
				}
				age := now.Sub(j.CreatedAt).Milliseconds()
				if age < 0 {
					age = 0
				}
				view.Jobs = append(view.Jobs, domain.QueueJobView{
					Seq: j.Seq, Status: j.Status, Alias: j.Alias, Model: j.AssignedModel,
					Backend: j.AssignedBackend, Provider: j.Provider, KeyPrefix: j.KeyPrefix,
					Error: j.Error, AgeMS: age,
				})
			}
		}
		out = append(out, view)
	}
	return out
}

func (e *Engine) Loop(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			e.purgeIfNeeded()
			qs, err := e.st.ListQueues()
			if err != nil {
				continue
			}
			for _, q := range qs {
				e.dispatch(q.ID)
			}
		}
	}
}

func (e *Engine) newJob(q domain.Queue, alias string, k domain.APIKey, path string, body []byte) *domain.QueueJob {
	return &domain.QueueJob{
		ID: newID(), QueueID: q.ID, Alias: alias, KeyPrefix: k.Prefix, Path: path, Body: body, Bytes: len(body),
	}
}

type headTracker struct {
	http.ResponseWriter
	wrote bool
}

func (h *headTracker) WriteHeader(code int) {
	h.wrote = true
	h.ResponseWriter.WriteHeader(code)
}

func (h *headTracker) Write(b []byte) (int, error) {
	if !h.wrote {
		h.WriteHeader(http.StatusOK)
	}
	return h.ResponseWriter.Write(b)
}

func (h *headTracker) Flush() {
	if f, ok := h.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func headersWritten(w http.ResponseWriter) bool {
	if h, ok := w.(*headTracker); ok {
		return h.wrote
	}
	return false
}

func writeJSON(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(`{"error":{"message":` + strconv.Quote(msg) + `}}`))
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func envInt64(k string, def int64) int64 {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

func envDuration(k string, def time.Duration) time.Duration {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return def
	}
	return d
}
