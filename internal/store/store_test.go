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
	bid, err := st.UpsertBackend("mac-82", "http://192.168.88.82:11434", true, 1)
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
