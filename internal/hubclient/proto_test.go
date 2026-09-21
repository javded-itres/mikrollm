package hubclient

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestCollapseChatToolCallsJSON(t *testing.T) {
	body := []byte(`{
		"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"kimi",
		"choices":[{"index":0,"finish_reason":"tool_calls","message":{
			"role":"assistant","content":null,
			"reasoning":"need weather",
			"tool_calls":[{"id":"call_1","index":0,"type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Moscow\"}"}}]
		}}],
		"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
	got := CollapseChat(body)
	var out struct {
		ID      string          `json:"id"`
		Model   string          `json:"model"`
		Usage   json.RawMessage `json:"usage"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content   string     `json:"content"`
				Reasoning string     `json:"reasoning"`
				ToolCalls []ToolCall `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(got, &out) != nil {
		t.Fatalf("collapse not JSON: %s", got)
	}
	if out.ID != "chatcmpl-1" || out.Model != "kimi" {
		t.Fatalf("id/model lost: %s", got)
	}
	if len(out.Usage) == 0 {
		t.Fatalf("usage lost: %s", got)
	}
	if len(out.Choices) != 1 || out.Choices[0].FinishReason != "tool_calls" {
		t.Fatalf("finish_reason lost: %s", got)
	}
	m := out.Choices[0].Message
	if m.Reasoning != "need weather" || m.Content != "" {
		t.Fatalf("message fields: %+v", m)
	}
	if len(m.ToolCalls) != 1 {
		t.Fatalf("tool_calls lost: %s", got)
	}
	tc := m.ToolCalls[0]
	if tc.ID != "call_1" || tc.Type != "function" || tc.Function.Name != "get_weather" || tc.Function.Arguments != `{"city":"Moscow"}` {
		t.Fatalf("tool call mangled: %+v", tc)
	}
}

func TestCollapseChatToolCallsSSE(t *testing.T) {
	sse := []byte(`data: {"id":"c1","model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":null,"tool_calls":[{"index":0,"id":"call_9","type":"function","function":{"name":"search","arguments":"{\"q\":"}}]}}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"погода"}},{"index":1,"function":{"name":"time","arguments":"{\"tz\":"}}]}}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"}"}},{"index":1,"function":{"arguments":"\"UTC\"}"}}]}}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"total_tokens":42}}

data: [DONE]
`)
	got := CollapseChat(sse)
	var out struct {
		ID    string `json:"id"`
		Usage struct {
			Total int `json:"total_tokens"`
		} `json:"usage"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content   string     `json:"content"`
				ToolCalls []ToolCall `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(got, &out) != nil {
		t.Fatalf("collapse not JSON: %s", got)
	}
	if out.ID != "c1" || out.Usage.Total != 42 || out.Choices[0].FinishReason != "tool_calls" {
		t.Fatalf("meta lost: %s", got)
	}
	tcs := out.Choices[0].Message.ToolCalls
	if len(tcs) != 2 {
		t.Fatalf("want 2 tool calls, got %d: %s", len(tcs), got)
	}
	if tcs[0].ID != "call_9" || tcs[0].Function.Name != "search" || tcs[0].Function.Arguments != `{"q":"погода"}` {
		t.Fatalf("call 0 bad: %+v", tcs[0])
	}
	if tcs[1].Function.Name != "time" || tcs[1].Function.Arguments != `{"tz":"UTC"}` {
		t.Fatalf("call 1 bad: %+v", tcs[1])
	}
}

func TestCollapseChatArgumentsAsObject(t *testing.T) {
	body := []byte(`{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","content":"",
		"tool_calls":[{"id":"x","type":"function","function":{"name":"f","arguments":{"a":1}}}]}}]}`)
	got := CollapseChat(body)
	if !bytes.Contains(got, []byte(`"arguments":"{\"a\":1}"`)) {
		t.Fatalf("object arguments not stringified: %s", got)
	}
}

func TestCollapseChatPassthroughNonChat(t *testing.T) {
	body := []byte(`{"error":{"message":"boom"}}`)
	if !bytes.Equal(CollapseChat(body), body) {
		t.Fatalf("error body mutated: %s", CollapseChat(body))
	}
}

func TestParseChatContentAndFinish(t *testing.T) {
	body := []byte(`{"id":"x","choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"hi","reasoning":"why"}}]}`)
	r := ParseChat(body)
	if r.Content != "hi" || r.Reasoning != "why" || r.FinishReason != "stop" || r.ID != "x" {
		t.Fatalf("%+v", r)
	}
	c, rr := ChatText(body)
	if c != "hi" || rr != "why" {
		t.Fatalf("ChatText %q %q", c, rr)
	}
}

func TestParseChatEmpty(t *testing.T) {
	if r := ParseChat(nil); r.Content != "" || len(r.ToolCalls) != 0 {
		t.Fatalf("empty body parsed: %+v", r)
	}
}
