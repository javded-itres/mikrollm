package store

import (
	"path/filepath"
	"testing"
	"time"
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
