package admin

import (
	"net/http"
	"net/url"
	"strings"
)

// uiText is the admin chrome and the Agents page. Other pages stay Russian.
var uiText = map[string]map[string]string{
	"ru": {
		"nav_status": "Статус", "nav_queues": "Очереди", "nav_models": "Модели", "nav_mcp": "MCP",
		"nav_agents": "Агенты", "nav_security": "Безопасность", "nav_keys": "Ключи", "nav_chat": "Чат",
		"nav_api": "API", "nav_logs": "Лог", "nav_billing": "Биллинг",
		"install": "В приложение", "logout": "Выйти",
		"job_pull": "Скачивание", "job_refresh": "обновление страницы не прерывает скачивание",
		"agents_title": "Агенты",
		"agents_lead":  "Соседей агент видит через GET /a2a/directory и пишет им POST /a2a/messages. Видны только общие группы.",
		"invite_title": "Приглашение", "invite_in": "в",
		"invite_hint": "Скопируйте сейчас — повторно не покажем. Старое приглашение этой группы больше не действует.",
		"copy":        "Копировать",
		"groups":      "Группы", "group_name": "имя", "group_title": "название", "group_about": "описание",
		"group_create": "Создать", "members": "Участники", "nobody": "пока никого",
		"invite_new": "Новое приглашение", "group_delete": "Удалить группу",
		"dialog": "Диалог", "no_messages": "Сообщений пока нет.",
		"status_pending": "ожидает", "status_rejected": "отклонено", "status_posted": "в чате",
		"to_group": "группа", "confirm": "Подтвердить", "reject": "Отклонить",
		"add_agent": "Добавить агента", "name": "Имя", "display": "Как показывать", "company": "Компания",
		"url": "URL", "url_ph": "не нужен — запрос придёт на соединение",
		"about": "Что умеет", "skills": "Навыки", "skills_hint": "Строка: id | название | описание",
		"token": "Bearer upstream", "token_ph": "если URL требует Bearer", "token_keep": "сохранён, пустое поле не меняет",
		"enabled": "Включён", "share": "В сеть", "own_key": "свой ключ", "add": "Добавить", "save": "Сохранить",
		"delete": "Удалить", "card": "Карточка", "call": "Вызов",
		"empty":        "Агентов пока нет. Создайте группу и отдайте приглашение, либо добавьте агента.",
		"share_off":    "«В сеть» включается на Статусе: участник hub и «Доступ к агентам».",
		"remote":       "С других узлов",
		"remote_off":   "Включите «Участник hub сети», чтобы видеть агентов соседей. Группы остаются на этом шлюзе.",
		"remote_empty": "Пока никто не открыл агентов в сеть.",
		"offline":      "не в сети",
		"on":           "вкл", "off": "выкл",
	},
	"en": {
		"nav_status": "Status", "nav_queues": "Queues", "nav_models": "Models", "nav_mcp": "MCP",
		"nav_agents": "Agents", "nav_security": "Security", "nav_keys": "Keys", "nav_chat": "Chat",
		"nav_api": "API", "nav_logs": "Log", "nav_billing": "Billing",
		"install": "Install app", "logout": "Log out",
		"job_pull": "Download", "job_refresh": "reloading the page does not stop the download",
		"agents_title": "Agents",
		"agents_lead":  "An agent lists peers with GET /a2a/directory and writes to them with POST /a2a/messages. Only shared groups are visible.",
		"invite_title": "Invite", "invite_in": "for",
		"invite_hint": "Copy it now. It is not shown again. The previous invite for this group no longer works.",
		"copy":        "Copy",
		"groups":      "Groups", "group_name": "name", "group_title": "title", "group_about": "description",
		"group_create": "Create", "members": "Members", "nobody": "none yet",
		"invite_new": "New invite", "group_delete": "Delete group",
		"dialog": "Dialog", "no_messages": "No messages yet.",
		"status_pending": "pending", "status_rejected": "rejected", "status_posted": "posted",
		"to_group": "group", "confirm": "Confirm", "reject": "Reject",
		"add_agent": "Add agent", "name": "Name", "display": "Display name", "company": "Company",
		"url": "URL", "url_ph": "optional — the call arrives on the open connection",
		"about": "What it does", "skills": "Skills", "skills_hint": "Line: id | name | description",
		"token": "Upstream bearer", "token_ph": "if the URL expects a bearer", "token_keep": "saved; leave blank to keep it",
		"enabled": "Enabled", "share": "Share", "own_key": "own key", "add": "Add", "save": "Save",
		"delete": "Delete", "card": "Card", "call": "Call",
		"empty":        "No agents yet. Create a group and share the invite, or add an agent here.",
		"share_off":    "Share turns on from Status: hub member and “Agent access”.",
		"remote":       "From other nodes",
		"remote_off":   "Turn on “Hub member” to see agents neighbors shared. Groups stay on this gateway.",
		"remote_empty": "No neighbor has shared an agent yet.",
		"offline":      "offline",
		"on":           "on", "off": "off",
	},
}

func (u *UI) setLang(w http.ResponseWriter, r *http.Request) {
	v := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("v")))
	if v != "en" {
		v = "ru"
	}
	http.SetCookie(w, &http.Cookie{
		Name: "lang", Value: v, Path: "/admin", MaxAge: 86400 * 400,
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, langNext(r), http.StatusSeeOther)
}

func requestLang(r *http.Request) string {
	if r == nil {
		return "ru"
	}
	c, err := r.Cookie("lang")
	if err != nil || c.Value != "en" {
		return "ru"
	}
	return "en"
}

func langNext(r *http.Request) string {
	ref, err := url.Parse(r.Referer())
	if err != nil || ref.Path == "" || !strings.HasPrefix(ref.Path, "/admin") || strings.HasPrefix(ref.Path, "/admin/lang") {
		return "/admin"
	}
	if ref.Host != "" && !strings.EqualFold(ref.Host, r.Host) {
		return "/admin"
	}
	if ref.RawQuery != "" {
		return ref.Path + "?" + ref.RawQuery
	}
	return ref.Path
}

func tr(lang any, key string) string {
	code, _ := lang.(string)
	if code != "en" {
		code = "ru"
	}
	if s, ok := uiText[code][key]; ok {
		return s
	}
	if s, ok := uiText["ru"][key]; ok {
		return s
	}
	return key
}
