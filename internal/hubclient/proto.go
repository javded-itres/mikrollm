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

type Caps struct {
	Chat   int `json:"chat"`
	Images int `json:"images"`
	Videos int `json:"videos"`
}

type AnnounceReq struct {
	Name     string         `json:"name"`
	Aliases  []Alias        `json:"aliases"`
	Schedule *ShareSchedule `json:"schedule,omitempty"`
	Caps     *Caps          `json:"caps,omitempty"`
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
	Caps       *Caps          `json:"caps,omitempty"`
}

type PoolEntry struct {
	NodeID   string   `json:"node_id"`
	NodeName string   `json:"node_name,omitempty"`
	Alias    string   `json:"alias"`
	Tier     string   `json:"tier,omitempty"`
	Context  int      `json:"context,omitempty"`
	Media    []string `json:"media,omitempty"`
}

type Defaults struct {
	NodeID   string      `json:"node_id"`
	NodeName string      `json:"node_name,omitempty"`
	Alias    string      `json:"alias"`
	Pool     []PoolEntry `json:"pool,omitempty"`
}

type Catalog struct {
	Online   int          `json:"online"`
	Total    int          `json:"total"`
	Defaults *Defaults    `json:"defaults,omitempty"`
	Nodes    []NodePublic `json:"nodes"`
}

// ToolCall is one function invocation requested by the model.
type ToolCall struct {
	ID       string       `json:"id,omitempty"`
	Index    int          `json:"index"`
	Type     string       `json:"type,omitempty"`
	Function ToolFunction `json:"function"`
}

// ToolFunction is the invoked function name plus its JSON arguments (a string per OpenAI spec).
type ToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ChatResult is a full chat completion reassembled from either a non-stream
// JSON response or a sequence of SSE chunks. Nothing the model produced is
// dropped: content, reasoning, tool calls, finish reason and usage.
type ChatResult struct {
	ID           string
	Model        string
	Content      string
	Reasoning    string
	ToolCalls    []ToolCall
	FinishReason string
	Usage        json.RawMessage

	found   bool
	callPos map[int]int
}

// toolCallRaw tolerates providers that send arguments as an object instead of a string.
type toolCallRaw struct {
	ID       string `json:"id"`
	Index    *int   `json:"index"`
	Type     string `json:"type"`
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

func (t toolCallRaw) idx() int {
	if t.Index == nil {
		return 0
	}
	return *t.Index
}

func (t toolCallRaw) arguments() string {
	raw := bytes.TrimSpace(t.Function.Arguments)
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return s
		}
		return ""
	}
	var buf bytes.Buffer
	if json.Compact(&buf, raw) == nil {
		return buf.String()
	}
	return ""
}

// ParseChat reads an OpenAI chat completion body (plain JSON or an SSE stream)
// and returns everything the model produced, merged across chunks.
func ParseChat(body []byte) ChatResult {
	var r ChatResult
	trim := bytes.TrimSpace(body)
	if len(trim) == 0 {
		return r
	}
	if json.Valid(trim) && !bytes.HasPrefix(trim, []byte("data:")) {
		r.mergeChunk(trim)
		return r
	}
	for _, line := range bytes.Split(body, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(line[5:])
		if bytes.Equal(data, []byte("[DONE]")) || len(data) == 0 {
			continue
		}
		r.mergeChunk(data)
	}
	return r
}

func (r *ChatResult) mergeChunk(raw []byte) {
	var j struct {
		ID      string          `json:"id"`
		Model   string          `json:"model"`
		Usage   json.RawMessage `json:"usage"`
		Choices []struct {
			FinishReason *string `json:"finish_reason"`
			Message      struct {
				Content          any           `json:"content"`
				Reasoning        any           `json:"reasoning"`
				ReasoningContent any           `json:"reasoning_content"`
				ToolCalls        []toolCallRaw `json:"tool_calls"`
			} `json:"message"`
			Delta struct {
				Content          any           `json:"content"`
				Reasoning        any           `json:"reasoning"`
				ReasoningContent any           `json:"reasoning_content"`
				ToolCalls        []toolCallRaw `json:"tool_calls"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if json.Unmarshal(raw, &j) != nil {
		return
	}
	if j.ID != "" && r.ID == "" {
		r.ID = j.ID
	}
	if j.Model != "" && r.Model == "" {
		r.Model = j.Model
	}
	if len(bytes.TrimSpace(j.Usage)) > 0 {
		r.Usage = j.Usage
	}
	if len(j.Choices) == 0 {
		return
	}
	r.found = true
	m, d := j.Choices[0].Message, j.Choices[0].Delta
	r.Content += anyText(m.Content) + anyText(d.Content)
	r.Reasoning += anyText(m.Reasoning) + anyText(m.ReasoningContent) + anyText(d.Reasoning) + anyText(d.ReasoningContent)
	r.mergeCalls(m.ToolCalls)
	r.mergeCalls(d.ToolCalls)
	if j.Choices[0].FinishReason != nil && *j.Choices[0].FinishReason != "" {
		r.FinishReason = *j.Choices[0].FinishReason
	}
}

func (r *ChatResult) mergeCalls(calls []toolCallRaw) {
	// positions maps stream index to the slot in r.ToolCalls.
	r.mergeIndexInit()
	for _, c := range calls {
		pos, ok := r.callPos[c.idx()]
		if !ok {
			pos = len(r.ToolCalls)
			r.callPos[c.idx()] = pos
			r.ToolCalls = append(r.ToolCalls, ToolCall{Index: c.idx()})
		}
		t := &r.ToolCalls[pos]
		if c.ID != "" {
			t.ID = c.ID
		}
		if c.Type != "" {
			t.Type = c.Type
		}
		if name := c.Function.Name; name != "" {
			if t.Function.Name == "" {
				t.Function.Name = name
			} else if name != t.Function.Name {
				// A few providers stream the name in fragments.
				t.Function.Name += name
			}
		}
		t.Function.Arguments += c.arguments()
	}
}

func (r *ChatResult) mergeIndexInit() {
	if r.callPos != nil {
		return
	}
	r.callPos = map[int]int{}
	for i, tc := range r.ToolCalls {
		r.callPos[tc.Index] = i
	}
}

// CollapseChat turns an OpenAI JSON completion or an SSE stream into one JSON
// object, preserving tool calls, finish reason, ids and usage. Bodies that do
// not look like a chat completion (errors, media payloads) pass through as-is.
func CollapseChat(body []byte) []byte {
	r := ParseChat(body)
	if !r.found {
		return bytes.TrimSpace(body)
	}
	msg := map[string]any{"role": "assistant", "content": r.Content}
	if r.Reasoning != "" {
		msg["reasoning"] = r.Reasoning
	}
	if len(r.ToolCalls) > 0 {
		msg["tool_calls"] = r.ToolCalls
	}
	choice := map[string]any{"index": 0, "message": msg}
	if r.FinishReason != "" {
		choice["finish_reason"] = r.FinishReason
	}
	resp := map[string]any{
		"object":  "chat.completion",
		"choices": []any{choice},
	}
	if r.ID != "" {
		resp["id"] = r.ID
	}
	if r.Model != "" {
		resp["model"] = r.Model
	}
	if len(r.Usage) > 0 {
		resp["usage"] = r.Usage
	}
	out, err := json.Marshal(resp)
	if err != nil {
		return body
	}
	return out
}

// ChatText extracts just the text (content and reasoning) of a completion.
func ChatText(body []byte) (content, reasoning string) {
	r := ParseChat(body)
	return r.Content, r.Reasoning
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
