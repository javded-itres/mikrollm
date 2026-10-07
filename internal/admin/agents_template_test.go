package admin

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAgentsTemplateShowsGroup(t *testing.T) {
	ui := New(Deps{})
	rec := httptest.NewRecorder()
	err := ui.pages["agents"].ExecuteTemplate(rec, "layout.html", map[string]any{
		"Title": "Агенты", "Nav": "agents", "CSRF": "t",
		"Rows": []agentRow{{
			ID: 1, Name: "holix", DisplayName: "Holix", SkillsText: "order | Заказ | товары",
			GroupIDs: []int64{1}, Enabled: true, Registered: true,
		}},
		"Groups": []groupView{{
			ID: 1, Name: "orders", Title: "Заказы", Members: []string{"holix", "shop"},
			Messages: []msgView{{ID: 7, From: "holix", To: "shop", Body: "подтвердите отправку", Status: "pending", Created: "t"}},
		}},
		"CanShare": false, "HubOn": false,
		"Invite": "inv-secret", "InviteGroup": "orders",
	})
	if err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()
	for _, want := range []string{"inv-secret", "holix", "Подтвердить", "свой ключ", "orders"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s", want)
		}
	}
}
