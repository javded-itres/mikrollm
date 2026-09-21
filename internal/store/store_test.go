package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/javded-itres/mikrollm/internal/domain"
)

func TestListModelsNoDeadlock(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.EnsureAdmin("secret", true); err != nil {
		t.Fatal(err)
	}
	bid, err := st.UpsertBackend("mac-82", "http://192.168.88.82:11434", true, 1, "ollama", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveModel(Model{
		Alias: "qwen", UpstreamName: "qwen", LBPolicy: "least_conn",
		Enabled: true, BackendIDs: []int64{bid},
	}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := st.ListModels()
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ListModels deadlocked (nested query with MaxOpenConns=1)")
	}
	ms, err := st.ListModels()
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || len(ms[0].BackendIDs) != 1 || ms[0].BackendIDs[0] != bid {
		t.Fatalf("%+v", ms)
	}
}

func TestBackendKindAndToken(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	id, err := st.UpsertBackend("gpu", "http://192.168.88.10:8000", true, 1, "v-llm", "Bearer secret")
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.GetBackend(id)
	if err != nil {
		t.Fatal(err)
	}
	if b.Kind != "vllm" || b.Token != "secret" || b.Name != "gpu" {
		t.Fatalf("%+v", b)
	}
	list, err := st.ListBackends()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Kind != "vllm" {
		t.Fatalf("%+v", list)
	}
	id2, err := st.UpsertBackend("gpu", "http://192.168.88.10:8000", true, 2, "lmstudio", "tok2")
	if err != nil {
		t.Fatal(err)
	}
	if id2 != id {
		t.Fatalf("upsert should keep id %d got %d", id, id2)
	}
	b, _ = st.GetBackend(id)
	if b.Kind != "lmstudio" || b.Token != "tok2" || b.Weight != 2 {
		t.Fatalf("%+v", b)
	}
}

func TestSetBackendEnabled(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	id, err := st.UpsertBackend("mac", "http://127.0.0.1:11434", true, 3, "ollama", "tok")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetBackendEnabled(id, false); err != nil {
		t.Fatal(err)
	}
	b, err := st.GetBackend(id)
	if err != nil {
		t.Fatal(err)
	}
	if b.Enabled || b.Weight != 3 || b.Token != "tok" {
		t.Fatalf("toggle must not touch other fields: %+v", b)
	}
	if err := st.SetBackendEnabled(id, true); err != nil {
		t.Fatal(err)
	}
	b, _ = st.GetBackend(id)
	if !b.Enabled {
		t.Fatal("want enabled")
	}
	if err := st.SetBackendEnabled(id+99, false); err == nil {
		t.Fatal("missing id")
	}
}

func TestPoliciesForDedup(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	id, err := st.SavePolicy(domain.Policy{
		Name: "once", Kind: domain.GuardBlockWords, Action: domain.GuardBlock, Mode: domain.GuardPre, Enabled: true,
		Config: domain.PolicyConfig{Words: []string{"x"}},
		Targets: []domain.PolicyTarget{
			{Kind: domain.GuardTargetAlias, Key: "coder"},
			{Kind: domain.GuardTargetQueue, Key: "coder"},
			{Kind: domain.GuardTargetModel, Key: "ornith-1.5:35b"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps, err := st.PoliciesFor("coder", "coder", "ornith-1.5:35b")
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 1 || ps[0].ID != id {
		t.Fatalf("want 1 policy, got %+v", ps)
	}
}

func TestModelContextAndKeyUpdate(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	bid, err := st.UpsertBackend("mac", "http://127.0.0.1:11434", true, 1, "ollama", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ConnectOllamaModel("qwen", []int64{bid}, "least_conn", 32768); err != nil {
		t.Fatal(err)
	}
	m, err := st.GetModelByAlias("qwen")
	if err != nil || m.MaxContext != 32768 {
		t.Fatalf("%+v %v", m, err)
	}
	m.MaxContext = 65536
	m.Fallback = "llama"
	if _, err := st.SaveModel(m); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetModel(m.ID)
	if got.MaxContext != 65536 || got.Fallback != "llama" {
		t.Fatalf("%+v", got)
	}
	if got.PromptCache != "inherit" {
		t.Fatalf("connect/save inherit, got %q", got.PromptCache)
	}
	id, err := st.InsertKey(APIKey{Name: "k", Prefix: "sk-abc", KeyHash: "h1", AllowedModels: []string{"qwen"}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	k, err := st.GetKey(id)
	if err != nil || k.AllowedModels[0] != "qwen" {
		t.Fatalf("%+v %v", k, err)
	}
	k.AllowedModels = []string{"*", "other"}
	k.RPM = 10
	if err := st.UpdateKey(k); err != nil {
		t.Fatal(err)
	}
	k2, _ := st.GetKey(id)
	if k2.RPM != 10 || k2.AllowedModels[0] != "*" {
		t.Fatalf("%+v", k2)
	}
}

func TestQueueCRUDAndAlias(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	id, err := st.SaveQueue(domain.Queue{
		Name: "home", Alias: "chat", Enabled: true, OverflowAfter: 5, OverflowAlias: "paid",
		Steps: []domain.QueueStep{{ModelAlias: "local", MaxConcurrent: 2}, {ModelAlias: "free-cloud", MaxConcurrent: 4}},
	})
	if err != nil {
		t.Fatal(err)
	}
	q, err := st.GetQueueByAlias("chat")
	if err != nil || len(q.Steps) != 2 || q.OverflowAfter != 5 {
		t.Fatalf("%+v %v", q, err)
	}
	if err := st.AddQueueAlias(id, "fast"); err != nil {
		t.Fatal(err)
	}
	q2, err := st.GetQueueByAlias("fast")
	if err != nil || q2.ID != id {
		t.Fatalf("%+v %v", q2, err)
	}
	taken, _ := st.AliasTaken("chat", 0, 0)
	if !taken {
		t.Fatal("chat should be taken")
	}
	if _, err := st.SaveModel(Model{Alias: "chat", UpstreamName: "x", Enabled: true}); err == nil {
		t.Fatal("model alias clash")
	}
}

func TestPromptCacheSettings(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.EnsureAdmin("x", true); err != nil {
		t.Fatal(err)
	}
	if st.PromptCacheMode() != "auto" {
		t.Fatalf("global %q", st.PromptCacheMode())
	}
	if err := st.SetPromptCacheMode("off"); err != nil {
		t.Fatal(err)
	}
	if st.PromptCacheMode() != "off" {
		t.Fatal(st.PromptCacheMode())
	}
	bid, err := st.UpsertBackend("mac", "http://127.0.0.1:11434", true, 1, "ollama", "")
	if err != nil {
		t.Fatal(err)
	}
	id, err := st.SaveModel(Model{Alias: "claude", UpstreamName: "anthropic/claude-sonnet-4", Enabled: true, BackendIDs: []int64{bid}, PromptCache: "on"})
	if err != nil {
		t.Fatal(err)
	}
	m, _ := st.GetModel(id)
	if m.PromptCache != "on" {
		t.Fatalf("%+v", m)
	}
	m.Fallback = "cheap"
	if _, err := st.SaveModel(m); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetModel(id)
	if got.PromptCache != "on" || got.Fallback != "cheap" {
		t.Fatalf("round-trip %+v", got)
	}
	if err := st.ConnectOllamaModel("qwen", []int64{bid}, "", 0); err != nil {
		t.Fatal(err)
	}
	q, _ := st.GetModelByAlias("qwen")
	if q.PromptCache != "inherit" {
		t.Fatalf("connect %q", q.PromptCache)
	}
	st.Log("sk-x", "claude", "or", 200, time.Millisecond, 10, domain.TokenUsage{
		PromptTokens: 100, CachedTokens: 80, Upstream: "anthropic/claude-sonnet-4",
		HasSaved: true, SavedUSD: 0.2, HasCost: true, Cost: 0.05,
	})
	ls, err := st.ListLogs(10)
	if err != nil || len(ls) != 1 || ls[0].CachedTokens != 80 || ls[0].SavedUSD != 0.2 || ls[0].UsageCost != 0.05 {
		t.Fatalf("%+v %v", ls, err)
	}
}

func TestHubSettingsAndShare(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.EnsureAdmin("x", true); err != nil {
		t.Fatal(err)
	}
	got, err := st.HubSettings()
	if err != nil || got.Enabled || got.Token != "" {
		t.Fatalf("%+v %v", got, err)
	}
	if got.Caps.Chat != 4 || got.Caps.Images != 2 || got.Caps.Videos != 1 {
		t.Fatalf("default caps %+v", got.Caps)
	}
	if err := st.SetHubSettings(domain.HubSettings{
		Enabled: true, NodeID: "n1", Token: "hk", Name: "hap",
		Schedule: domain.HubSchedule{Enabled: true, Days: []int{1, 3, 0}, Start: "00:00", End: "12:00", TZ: "UTC"},
	}); err != nil {
		t.Fatal(err)
	}
	got, err = st.HubSettings()
	if err != nil || !got.Enabled || got.NodeID != "n1" || got.Token != "hk" || got.Name != "hap" {
		t.Fatalf("%+v %v", got, err)
	}
	if !got.Schedule.Enabled || got.Schedule.Start != "00:00" || len(got.Schedule.Days) != 3 {
		t.Fatalf("schedule %+v", got.Schedule)
	}
	bid, err := st.UpsertBackend("mac", "http://127.0.0.1:11434", true, 1, "ollama", "")
	if err != nil {
		t.Fatal(err)
	}
	id, err := st.SaveModel(Model{Alias: "coder", UpstreamName: "qwen", Enabled: true, BackendIDs: []int64{bid}, HubShare: true})
	if err != nil {
		t.Fatal(err)
	}
	m, _ := st.GetModel(id)
	if !m.HubShare {
		t.Fatal("hub_share not stored")
	}
	m.HubShare = false
	if _, err := st.SaveModel(m); err != nil {
		t.Fatal(err)
	}
	m, _ = st.GetModel(id)
	if m.HubShare {
		t.Fatal("hub_share stuck")
	}
	if err := st.ConnectHubModel("coder", "n1", "ams-1", 8192, []string{"chat"}); err != nil {
		t.Fatal(err)
	}
	hm, err := st.GetModelByAlias("coder@ams-1")
	if err != nil {
		hm, err = st.GetModelByAlias("coder")
	}
	if err != nil || hm.HubNodeID != "n1" {
		t.Fatalf("hub model %+v %v", hm, err)
	}
}

func TestUpsertHubAuto(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.EnsureAdmin("x", true); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertHubAuto("n1", "ams-1", "coder", 8192, []string{"chat"}); err != nil {
		t.Fatal(err)
	}
	m, err := st.GetModelByAlias("auto")
	if err != nil || m.HubNodeID != "n1" || m.UpstreamName != "coder" || m.HubShare {
		t.Fatalf("%+v %v", m, err)
	}
	if err := st.UpsertHubAuto("n2", "hap", "qwen", 4096, []string{"chat"}); err != nil {
		t.Fatal(err)
	}
	m, err = st.GetModelByAlias("auto")
	if err != nil || m.HubNodeID != "n2" || m.UpstreamName != "qwen" || m.Alias != "auto" {
		t.Fatalf("update %+v %v", m, err)
	}
	if err := st.DeleteHubAuto(); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetModelByAlias("auto"); err == nil {
		t.Fatal("auto still there")
	}
}

func TestMaxAliasCtx(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	save := func(m Model) {
		if _, err := st.SaveModel(m); err != nil {
			t.Fatal(err)
		}
	}
	save(Model{Alias: "coder", UpstreamName: "qwen3:32b", Params: `{"num_ctx":65536}`, Enabled: true})
	save(Model{Alias: "writer", UpstreamName: "qwen3:32b", Params: `{"num_ctx":32768}`, Enabled: true})
	save(Model{Alias: "other", UpstreamName: "llama3", MaxContext: 4096, Enabled: true})

	n, err := st.MaxAliasCtx("qwen3:32b")
	if err != nil || n != 65536 {
		t.Fatalf("want 65536 got %d %v", n, err)
	}
	// alias-name match wins too, MaxContext used when no profile
	save(Model{Alias: "qwen3:32b", UpstreamName: "qwen3:32b", MaxContext: 131072, Enabled: true})
	n, _ = st.MaxAliasCtx("qwen3:32b")
	if n != 131072 {
		t.Fatalf("MaxContext fallback: %d", n)
	}
	// disabled alias is skipped
	m, _ := st.GetModelByAlias("coder")
	m.Enabled = false
	st.SaveModel(m)
	n, _ = st.MaxAliasCtx("qwen3:32b")
	if n != 131072 {
		t.Fatalf("after disable: %d", n)
	}
	if n, _ = st.MaxAliasCtx("unknown"); n != 0 {
		t.Fatalf("unknown model: %d", n)
	}
}
