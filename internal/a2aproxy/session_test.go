package a2aproxy

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestConnectDeliversWithoutURL(t *testing.T) {
	st := openTestStore(t)
	mustGroup(t, st, "desk", "inv-desk")
	mux := http.NewServeMux()
	MountGroups(mux, st)
	New(st, func(http.ResponseWriter, *http.Request) bool { return true }, func() string { return "" }).Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	clerk := registerAgent(t, mux, "inv-desk", map[string]any{
		"name": "clerk", "display_name": "Clerk", "description": "зовёт",
	}, true)
	bot := registerAgent(t, mux, "inv-desk", map[string]any{
		"name": "bot", "display_name": "Bot", "description": "слушает",
	}, true)

	got := make(chan string, 1)
	go func() {
		got <- connectOnce(t, srv, bot.token)
	}()
	postMsg(t, mux, clerk.token, map[string]any{"group": "desk", "to": "bot", "text": "на связи"}, http.StatusCreated)
	select {
	case body := <-got:
		if !strings.Contains(body, "message/send") || !strings.Contains(body, "на связи") || strings.Contains(body, bot.token) {
			t.Fatalf("body %s", body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("connect timed out")
	}

	answered := make(chan struct{})
	go func() {
		raw := connectOnce(t, srv, bot.token)
		req, err := http.NewRequest(http.MethodPost, srv.URL+"/a2a/connect", strings.NewReader(replyFor(raw)))
		if err != nil {
			t.Errorf("reply req %v", err)
			return
		}
		req.Header.Set("Authorization", "Bearer "+bot.token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Errorf("reply %v", err)
			return
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Errorf("reply status %d", resp.StatusCode)
		}
		close(answered)
	}()

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/a2a/u/bot", strings.NewReader(`{"jsonrpc":"2.0","id":"9","method":"message/send","params":{"message":{"parts":[{"kind":"text","text":"ping"}]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(string(raw), `"ok":true`) {
		t.Fatalf("rpc %d %s", resp.StatusCode, raw)
	}
	select {
	case <-answered:
	case <-time.After(3 * time.Second):
		t.Fatal("agent did not answer")
	}
}

func connectOnce(t *testing.T, srv *httptest.Server, token string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/a2a/connect", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("connect %d %s", resp.StatusCode, raw)
	}
	return string(raw)
}

func replyFor(request string) string {
	id := rpcID([]byte(request))
	if id == "" {
		id = `"0"`
	}
	var b bytes.Buffer
	b.WriteString(`{"jsonrpc":"2.0","id":`)
	b.WriteString(id)
	b.WriteString(`,"result":{"ok":true}}`)
	return b.String()
}
