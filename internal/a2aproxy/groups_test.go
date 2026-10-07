package a2aproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/javded-itres/mikrollm/internal/auth"
	"github.com/javded-itres/mikrollm/internal/domain"
	"github.com/javded-itres/mikrollm/internal/store"
)

func TestGroupsIsolateAndConfirm(t *testing.T) {
	st := openTestStore(t)
	orders := mustGroup(t, st, "orders", "inv-orders")
	ships := mustGroup(t, st, "ships", "inv-ships")
	_ = orders
	_ = ships

	var hits atomic.Int32
	var gotAuth, gotBody string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":{"id":"task"}}`))
	}))
	t.Cleanup(up.Close)

	mux := http.NewServeMux()
	MountGroups(mux, st)

	holix := registerAgent(t, mux, "inv-orders", map[string]any{
		"name": "holix", "display_name": "Holix", "company": "Holix",
		"description": "Диалог по заказу",
		"skills":      []any{map[string]any{"id": "order", "name": "Заказ", "description": "ведёт заказ"}},
	}, true)
	shop := registerAgent(t, mux, "inv-orders", map[string]any{
		"name": "shop", "display_name": "Магазин", "company": "Shop",
		"description": "Товары", "skills": []any{map[string]any{"id": "goods", "name": "Товары", "description": "каталог"}},
	}, true)
	logist := registerAgent(t, mux, "inv-ships", map[string]any{
		"name": "logist", "display_name": "Логист", "company": "Другая",
		"description": "Доставка",
	}, true)

	again := registerAgent(t, mux, "inv-ships", map[string]any{
		"name": "holix", "token": holix.token, "display_name": "Holix", "company": "Holix",
		"description": "Диалог по заказу",
		"skills":      []any{map[string]any{"id": "order", "name": "Заказ", "description": "ведёт заказ"}},
	}, false)
	if again.token != "" {
		t.Fatal("token reissued")
	}
	if !containsAll(again.groups, "orders", "ships") {
		t.Fatalf("groups %#v", again.groups)
	}

	rec := callJSON(t, mux, http.MethodPost, "/a2a/register", "", map[string]any{"invite": "inv-orders", "name": "shop"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("taken %d %s", rec.Code, rec.Body.String())
	}

	dir := directory(t, mux, holix.token)
	if !hasAgent(dir, "shop") || !hasAgent(dir, "logist") {
		t.Fatalf("holix sees %#v", dir["agents"])
	}
	if shopPeer := peer(dir, "shop"); !containsAll(shopPeer, "orders") || containsAll(shopPeer, "ships") {
		t.Fatalf("shop groups %#v", shopPeer)
	}
	shopDir := directory(t, mux, shop.token)
	if hasAgent(shopDir, "logist") || !hasAgent(shopDir, "holix") {
		t.Fatalf("shop sees %#v", shopDir["agents"])
	}
	logDir := directory(t, mux, logist.token)
	if hasAgent(logDir, "shop") || !hasAgent(logDir, "holix") {
		t.Fatalf("logist sees %#v", logDir["agents"])
	}

	row, err := st.GetA2AByName("shop")
	if err != nil {
		t.Fatal(err)
	}
	row.URL = up.URL + "/rpc"
	row.Token = "up-secret"
	if _, err := st.SaveA2A(row); err != nil {
		t.Fatal(err)
	}
	kept, err := st.GetA2AByName("shop")
	if err != nil || kept.AuthHash == "" || kept.Token != "up-secret" {
		t.Fatalf("auth lost %+v %v", kept, err)
	}
	blank := kept
	blank.Token = ""
	blank.AuthHash = ""
	blank.DisplayName = "Магазин 2"
	if _, err := st.SaveA2A(blank); err != nil {
		t.Fatal(err)
	}
	kept, _ = st.GetA2AByName("shop")
	if kept.Token != "up-secret" || kept.AuthHash == "" || kept.DisplayName != "Магазин 2" {
		t.Fatalf("preserve %+v", kept)
	}

	postMsg(t, mux, holix.token, map[string]any{"group": "orders", "to": "logist", "text": "вези"}, http.StatusForbidden)
	hold := postMsg(t, mux, holix.token, map[string]any{"group": "orders", "to": "shop", "text": "подтвердите отправку", "confirm": true}, http.StatusCreated)
	if hold["status"] != "pending" {
		t.Fatalf("hold %#v", hold)
	}
	if hits.Load() != 0 {
		t.Fatal("pending was forwarded")
	}
	feed := messages(t, mux, shop.token, "orders")
	if strings.Contains(feed, "подтвердите") {
		t.Fatalf("shop saw pending %s", feed)
	}
	own := messages(t, mux, holix.token, "orders")
	if !strings.Contains(own, "подтвердите") {
		t.Fatalf("sender missed pending %s", own)
	}

	id := int64(hold["id"].(float64))
	if err := st.SetA2AMessageStatus(id, domain.A2APosted); err != nil {
		t.Fatal(err)
	}
	msg, err := st.GetA2AMessage(id)
	if err != nil {
		t.Fatal(err)
	}
	msg.Group = "orders"
	ForwardPosted(context.Background(), []domain.A2AUpstream{kept}, msg)
	if hits.Load() != 1 || gotAuth != "Bearer up-secret" {
		t.Fatalf("hits %d auth %q", hits.Load(), gotAuth)
	}
	if !strings.Contains(gotBody, "message/send") || !strings.Contains(gotBody, "group:orders:thread:main") || !strings.Contains(gotBody, "подтвердите отправку") || strings.Contains(gotBody, holix.token) {
		t.Fatalf("body %s", gotBody)
	}
	feed = messages(t, mux, shop.token, "orders")
	if !strings.Contains(feed, "подтвердите") {
		t.Fatalf("posted hidden %s", feed)
	}

	sent := postMsg(t, mux, holix.token, map[string]any{"group": "ships", "to": "logist", "text": "заберите заказ"}, http.StatusCreated)
	if sent["status"] != "posted" {
		t.Fatalf("sent %#v", sent)
	}
	if strings.Contains(messages(t, mux, shop.token, "orders"), "заберите") {
		t.Fatal("shop saw the other group")
	}
	if !strings.Contains(messages(t, mux, logist.token, "ships"), "заберите") {
		t.Fatal("logist missed the group chat")
	}
}

type regResult struct {
	token  string
	groups []string
}

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func mustGroup(t *testing.T, st *store.Store, name, invite string) int64 {
	t.Helper()
	id, err := st.SaveA2AGroup(domain.A2AGroup{Name: name, Title: name, InviteHash: auth.HashKey(invite)})
	if err != nil || id == 0 {
		t.Fatal(err)
	}
	return id
}

func registerAgent(t *testing.T, mux *http.ServeMux, invite string, body map[string]any, wantToken bool) regResult {
	t.Helper()
	body["invite"] = invite
	rec := callJSON(t, mux, http.MethodPost, "/a2a/register", "", body)
	want := http.StatusOK
	if wantToken {
		want = http.StatusCreated
	}
	if rec.Code != want {
		t.Fatalf("register %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Token  string   `json:"token"`
		Groups []string `json:"groups"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if wantToken && !strings.HasPrefix(resp.Token, "agt-") {
		t.Fatalf("token %q", resp.Token)
	}
	return regResult{token: resp.Token, groups: resp.Groups}
}

func directory(t *testing.T, mux *http.ServeMux, token string) map[string]any {
	t.Helper()
	rec := callJSON(t, mux, http.MethodGet, "/a2a/directory", token, nil)
	if rec.Code != 200 {
		t.Fatalf("dir %d %s", rec.Code, rec.Body.String())
	}
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func hasAgent(dir map[string]any, name string) bool {
	list, _ := dir["agents"].([]any)
	for _, item := range list {
		row, _ := item.(map[string]any)
		if row["name"] == name {
			return true
		}
	}
	return false
}

func peer(dir map[string]any, name string) []string {
	list, _ := dir["agents"].([]any)
	for _, item := range list {
		row, _ := item.(map[string]any)
		if row["name"] != name {
			continue
		}
		raw, _ := row["groups"].([]any)
		var out []string
		for _, g := range raw {
			out = append(out, g.(string))
		}
		return out
	}
	return nil
}

func containsAll(groups []string, want ...string) bool {
	set := map[string]bool{}
	for _, g := range groups {
		set[g] = true
	}
	for _, w := range want {
		if !set[w] {
			return false
		}
	}
	return true
}

func postMsg(t *testing.T, mux *http.ServeMux, token string, body map[string]any, want int) map[string]any {
	t.Helper()
	rec := callJSON(t, mux, http.MethodPost, "/a2a/messages", token, body)
	if rec.Code != want {
		t.Fatalf("msg %d %s", rec.Code, rec.Body.String())
	}
	if want >= 400 {
		return nil
	}
	var resp struct {
		Message map[string]any `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp.Message
}

func messages(t *testing.T, mux *http.ServeMux, token, group string) string {
	t.Helper()
	rec := callJSON(t, mux, http.MethodGet, "/a2a/messages?group="+group+"&after=0", token, nil)
	if rec.Code != 200 {
		t.Fatalf("feed %d %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func callJSON(t *testing.T, mux *http.ServeMux, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rdr = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, rdr)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}
