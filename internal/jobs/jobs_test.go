package jobs

import (
	"path/filepath"
	"testing"

	"github.com/javded-itres/mikrollm/internal/store"
)

func TestNDJSONPercent(t *testing.T) {
	tr := New(nil)
	j, busy, err := tr.Begin("pull", 1, "mac-82", "qwen:latest")
	if err != nil || busy != nil {
		t.Fatalf("%v %v", err, busy)
	}
	w := tr.Writer(j.ID)
	_, _ = w.Write([]byte("{\"status\":\"downloading\",\"total\":100,\"completed\":40}\n"))
	_, _ = w.Write([]byte("{\"status\":\"success\"}\n"))
	_ = w.Close()
	list := tr.List()
	if len(list) != 1 || list[0].Percent != 100 || list[0].Message != "success" {
		t.Fatalf("%+v", list)
	}
}

func TestBusySameBackend(t *testing.T) {
	tr := New(nil)
	_, _, err := tr.Begin("pull", 1, "mac-82", "a")
	if err != nil {
		t.Fatal(err)
	}
	_, busy, err := tr.Begin("load", 1, "mac-82", "b")
	if !IsBusy(err) || busy == nil || busy.Model != "a" {
		t.Fatalf("%v %+v", err, busy)
	}
	_, _, err = tr.Begin("load", 2, "mac-80", "b")
	if err != nil {
		t.Fatal(err)
	}
}

func TestPersistRestore(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	a := New(st)
	j, _, err := a.Begin("pull", 2, "mac-80", "qwen:latest")
	if err != nil {
		t.Fatal(err)
	}
	w := a.Writer(j.ID)
	_, _ = w.Write([]byte("{\"status\":\"downloading\",\"total\":200,\"completed\":50}\n"))
	_ = w.Close()
	b := New(st)
	list := b.List()
	if len(list) == 0 || list[0].Model != "qwen:latest" || list[0].Percent != 25 {
		t.Fatalf("%+v", list)
	}
	if list[0].Status != "running" {
		t.Fatalf("status %s", list[0].Status)
	}
}
