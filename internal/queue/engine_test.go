package queue

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javded-itres/mikrollm/internal/domain"
	"github.com/javded-itres/mikrollm/internal/store"
)

type fakeRouter struct {
	mu    sync.Mutex
	busy  map[string]int
	delay time.Duration
	hits  []string
	block chan struct{}
}

func (f *fakeRouter) TryRoute(model string) (string, string, string, error) {
	return "srv-" + model, "Prov", model, nil
}

func (f *fakeRouter) Forward(ctx context.Context, w http.ResponseWriter, k domain.APIKey, path string, body []byte, model string) (string, string, int, error) {
	f.mu.Lock()
	f.hits = append(f.hits, model)
	f.mu.Unlock()
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return "srv-" + model, "Prov", 499, ctx.Err()
		}
	} else if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return "srv-" + model, "Prov", 499, ctx.Err()
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	_, _ = w.Write([]byte(`{"ok":"` + model + `"}`))
	return "srv-" + model, "Prov", 200, nil
}

func setupEng(t *testing.T, r Router) (*store.Store, *Engine, int64) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "q.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	id, err := st.SaveQueue(domain.Queue{
		Name: "chat", Alias: "chat", Enabled: true, OverflowAfter: 5,
		Steps: []domain.QueueStep{
			{ModelAlias: "local", MaxConcurrent: 1},
			{ModelAlias: "cloud", MaxConcurrent: 1},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	e := New(st, r, Limits{MaxBytes: 1 << 20, MaxJobs: 50, MaxWait: 2 * time.Second})
	return st, e, id
}

func TestQueuePrefersFirstStepThenOverflowsToSecond(t *testing.T) {
	block := make(chan struct{})
	r := &fakeRouter{block: block}
	_, e, _ := setupEng(t, r)
	var n int32
	go func() {
		rec := httptest.NewRecorder()
		e.Handle(context.Background(), rec, domain.APIKey{Prefix: "sk-a", AllowedModels: []string{"*"}}, "/v1/chat/completions", []byte(`{"model":"chat"}`), "chat")
		atomic.AddInt32(&n, 1)
		if rec.Code != 200 || rec.Body.String() != `{"ok":"local"}` {
			t.Errorf("first %d %s", rec.Code, rec.Body.String())
		}
	}()
	time.Sleep(40 * time.Millisecond)
	go func() {
		rec := httptest.NewRecorder()
		e.Handle(context.Background(), rec, domain.APIKey{Prefix: "sk-b", AllowedModels: []string{"*"}}, "/v1/chat/completions", []byte(`{"model":"chat"}`), "chat")
		atomic.AddInt32(&n, 1)
		if rec.Code != 200 || rec.Body.String() != `{"ok":"cloud"}` {
			t.Errorf("second %d %s", rec.Code, rec.Body.String())
		}
	}()
	time.Sleep(60 * time.Millisecond)
	close(block)
	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(&n) < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if atomic.LoadInt32(&n) != 2 {
		t.Fatalf("finished %d hits %v", n, r.hits)
	}
	if len(r.hits) < 2 || r.hits[0] != "local" || r.hits[1] != "cloud" {
		t.Fatalf("hits %v", r.hits)
	}
}

func TestQueueWaitsWhenAllBusyThenReleasesSameConn(t *testing.T) {
	block := make(chan struct{})
	r := &fakeRouter{block: block}
	st, e, id := setupEng(t, r)
	_, _ = st.SaveQueue(domain.Queue{
		ID: id, Name: "chat", Alias: "chat", Enabled: true,
		Steps: []domain.QueueStep{{ModelAlias: "local", MaxConcurrent: 1}},
	})
	var first, second string
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		rec := httptest.NewRecorder()
		e.Handle(context.Background(), rec, domain.APIKey{Prefix: "sk-a", AllowedModels: []string{"*"}}, "/v1/chat/completions", []byte(`{"n":1}`), "chat")
		first = rec.Body.String()
	}()
	time.Sleep(40 * time.Millisecond)
	go func() {
		defer wg.Done()
		rec := httptest.NewRecorder()
		e.Handle(context.Background(), rec, domain.APIKey{Prefix: "sk-b", AllowedModels: []string{"*"}}, "/v1/chat/completions", []byte(`{"n":2}`), "chat")
		second = rec.Body.String()
	}()
	time.Sleep(50 * time.Millisecond)
	snap := e.Snapshot()
	if len(snap) == 0 || snap[0].Waiting+snap[0].Running < 2 {
		t.Fatalf("snapshot %+v", snap)
	}
	close(block)
	wg.Wait()
	if first != `{"ok":"local"}` || second != `{"ok":"local"}` {
		t.Fatalf("bodies %q %q", first, second)
	}
}

func TestQueueOverflowAfterWaiting(t *testing.T) {
	block := make(chan struct{})
	r := &fakeRouter{block: block}
	st, e, id := setupEng(t, r)
	_, err := st.SaveQueue(domain.Queue{
		ID: id, Name: "chat", Alias: "chat", Enabled: true, OverflowAfter: 1, OverflowAlias: "paid",
		Steps: []domain.QueueStep{{ModelAlias: "local", MaxConcurrent: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		rec := httptest.NewRecorder()
		e.Handle(context.Background(), rec, domain.APIKey{Prefix: "a", AllowedModels: []string{"*"}}, "/v1/chat/completions", []byte(`{}`), "chat")
	}()
	time.Sleep(30 * time.Millisecond)
	go func() {
		defer wg.Done()
		rec := httptest.NewRecorder()
		e.Handle(context.Background(), rec, domain.APIKey{Prefix: "b", AllowedModels: []string{"*"}}, "/v1/chat/completions", []byte(`{}`), "chat")
	}()
	time.Sleep(30 * time.Millisecond)
	go func() {
		defer wg.Done()
		rec := httptest.NewRecorder()
		e.Handle(context.Background(), rec, domain.APIKey{Prefix: "c", AllowedModels: []string{"*"}}, "/v1/chat/completions", []byte(`{}`), "chat")
		if rec.Body.String() != `{"ok":"paid"}` {
			t.Errorf("overflow body %s", rec.Body.String())
		}
	}()
	time.Sleep(40 * time.Millisecond)
	close(block)
	wg.Wait()
	foundPaid := false
	for _, h := range r.hits {
		if h == "paid" {
			foundPaid = true
		}
	}
	if !foundPaid {
		t.Fatalf("expected overflow to paid, hits %v", r.hits)
	}
}

func TestQueueLookupExtraAlias(t *testing.T) {
	r := &fakeRouter{}
	st, e, id := setupEng(t, r)
	if err := st.AddQueueAlias(id, "fast"); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.Lookup("fast"); !ok {
		t.Fatal("fast alias")
	}
	rec := httptest.NewRecorder()
	e.Handle(context.Background(), rec, domain.APIKey{Prefix: "a", AllowedModels: []string{"*"}}, "/v1/chat/completions", []byte(`{}`), "fast")
	if rec.Code != 200 {
		t.Fatalf("code %d %s", rec.Code, rec.Body.String())
	}
}

func TestQueueLookupNameDashAlias(t *testing.T) {
	r := &fakeRouter{}
	st, e, _ := setupEng(t, r)
	if _, err := st.SaveQueue(domain.Queue{
		Name: "itres", Alias: "coder", Enabled: true,
		Steps: []domain.QueueStep{{ModelAlias: "local", MaxConcurrent: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	q, ok := e.Lookup("itres-coder")
	if !ok || q.Alias != "coder" {
		t.Fatalf("itres-coder %+v %v", q, ok)
	}
	if _, ok := e.Lookup("itres/coder"); !ok {
		t.Fatal("slash")
	}
	if _, ok := e.Lookup("itres"); !ok {
		t.Fatal("name")
	}
}

var _ io.Writer = httptest.NewRecorder()
