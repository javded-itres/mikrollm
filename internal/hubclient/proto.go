package hubclient

import (
	"bytes"
	"encoding/json"
	"strings"
)

// DefaultURL is compiled into MikroLLM. Override with MIKROLLM_HUB_URL for a local hub.
const DefaultURL = "https://hub.mikrollm.ru"

// RelayHeader is set on the in-process /v1/chat/completions call so the job
// does not need an sk- key.
const RelayHeader = "X-MikroLLM-Hub-Relay"

type Alias struct {
	Alias   string   `json:"alias"`
	Media   []string `json:"media,omitempty"`
	Context int      `json:"context,omitempty"`
}

type RegisterReq struct {
	Name string `json:"name"`
}

type RegisterRes struct {
	NodeID string `json:"node_id"`
	Token  string `json:"token"`
	Hub    string `json:"hub"`
}

type ShareSchedule struct {
	Enabled bool   `json:"enabled"`
	Days    []int  `json:"days,omitempty"`
	Start   string `json:"start,omitempty"`
	End     string `json:"end,omitempty"`
	TZ      string `json:"tz,omitempty"`
}

type AnnounceReq struct {
	Name     string         `json:"name"`
	Aliases  []Alias        `json:"aliases"`
	Schedule *ShareSchedule `json:"schedule,omitempty"`
}

type Job struct {
	ID    string          `json:"job_id"`
	Alias string          `json:"alias"`
	Kind  string          `json:"kind,omitempty"` // chat (default), images, videos, videos_status, videos_content
	Ref   string          `json:"ref,omitempty"`  // upstream video id
	Body  json.RawMessage `json:"body"`
}

type Result struct {
	JobID       string          `json:"job_id"`
	Status      int             `json:"status"`
	Body        json.RawMessage `json:"body,omitempty"`
	ContentType string          `json:"content_type,omitempty"`
	B64         string          `json:"b64,omitempty"`
}

type NodePublic struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Online     bool           `json:"online"`
	Aliases    []Alias        `json:"aliases"`
	Rating     int            `json:"rating,omitempty"`
	Schedule   *ShareSchedule `json:"schedule,omitempty"`
	SharingNow bool           `json:"sharing_now"`
}

type Defaults struct {
	NodeID   string `json:"node_id"`
	NodeName string `json:"node_name,omitempty"`
	Alias    string `json:"alias"`
}

type Catalog struct {
	Online   int          `json:"online"`
	Total    int          `json:"total"`
	Defaults *Defaults    `json:"defaults,omitempty"`
	Nodes    []NodePublic `json:"nodes"`
}

// CollapseChat turns an OpenAI JSON completion or an SSE stream into one JSON object.
func CollapseChat(body []byte) []byte {
	content, reasoning := ChatText(body)
	if content == "" && reasoning == "" {
		return bytes.TrimSpace(body)
	}
	msg := map[string]any{"role": "assistant", "content": content}
	if reasoning != "" {
		msg["reasoning"] = reasoning
	}
	out, err := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"message": msg}},
	})
	if err != nil {
		return body
	}
	return out
}

func ChatText(body []byte) (content, reasoning string) {
	trim := bytes.TrimSpace(body)
	if len(trim) == 0 {
		return "", ""
	}
	if json.Valid(trim) && !bytes.HasPrefix(trim, []byte("data:")) {
		return chatParts(trim)
	}
	var c, r strings.Builder
	for _, line := range bytes.Split(body, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(line[5:])
		if bytes.Equal(data, []byte("[DONE]")) || len(data) == 0 {
			continue
		}
		cc, rr := chatParts(data)
		c.WriteString(cc)
		r.WriteString(rr)
	}
	return c.String(), r.String()
}

func chatParts(raw []byte) (content, reasoning string) {
	var j struct {
		Choices []struct {
			Message struct {
				Content          any `json:"content"`
				Reasoning        any `json:"reasoning"`
				ReasoningContent any `json:"reasoning_content"`
			} `json:"message"`
			Delta struct {
				Content          any `json:"content"`
				Reasoning        any `json:"reasoning"`
				ReasoningContent any `json:"reasoning_content"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if json.Unmarshal(raw, &j) != nil || len(j.Choices) == 0 {
		return "", ""
	}
	m, d := j.Choices[0].Message, j.Choices[0].Delta
	content = anyText(m.Content)
	if content == "" {
		content = anyText(d.Content)
	}
	reasoning = anyText(m.Reasoning) + anyText(m.ReasoningContent) + anyText(d.Reasoning) + anyText(d.ReasoningContent)
	return content, reasoning
}

func anyText(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []any:
		var b strings.Builder
		for _, p := range t {
			if m, ok := p.(map[string]any); ok {
				if s, ok := m["text"].(string); ok {
					b.WriteString(s)
				}
			}
			if s, ok := p.(string); ok {
				b.WriteString(s)
			}
		}
		return b.String()
	default:
		return ""
	}
}
