package a2aproxy

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/javded-itres/mikrollm/internal/domain"
	"github.com/javded-itres/mikrollm/internal/mcpproxy"
)

// ForwardPosted pushes a transcript line as A2A message/send.
// Every enabled agent receives it on GET /a2a/connect. A saved URL is also called.
// A failed URL push leaves the transcript and the connection queue in place.
func ForwardPosted(ctx context.Context, agents []domain.A2AUpstream, msg domain.A2AMessage) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	body := messageSendBody(msg)
	var wg sync.WaitGroup
	for _, a := range agents {
		if !a.Enabled {
			continue
		}
		PushAgent(a.Name, body, nil)
		if strings.TrimSpace(a.URL) == "" {
			continue
		}
		wg.Add(1)
		go func(a domain.A2AUpstream) {
			defer wg.Done()
			target, ok := Join(a.URL, "")
			if !ok {
				return
			}
			_, _ = call(ctx, a.Token, target, mcpproxy.Forward{
				Method: http.MethodPost, ContentType: "application/json", Accept: "application/json", Body: body,
			})
		}(a)
	}
	wg.Wait()
}

func messageSendBody(msg domain.A2AMessage) []byte {
	id := strconv.FormatInt(msg.ID, 10)
	group := msg.Group
	if group == "" {
		group = "group"
	}
	raw, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "message/send",
		"params": map[string]any{
			"message": map[string]any{
				"role":      "user",
				"messageId": "m" + id,
				"contextId": "group:" + group + ":thread:main",
				"parts":     []any{map[string]any{"kind": "text", "text": msg.Body}},
				"metadata": map[string]any{
					"mikrollm": map[string]any{"from": msg.FromAgent, "to": msg.ToAgent, "group": group},
				},
			},
		},
	})
	return raw
}
