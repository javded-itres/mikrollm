package a2aproxy

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

const agentQueueMax = 32

// connectWait is how long GET /a2a/connect stays open when the agent has no call yet.
const connectWait = 25 * time.Second

type queuedCall struct {
	id    string
	body  []byte
	reply chan []byte
}

type agentBox struct {
	mu        sync.Mutex
	q         []queuedCall
	waiter    chan []byte
	waitReply map[string]chan []byte
}

var (
	sessionMu sync.Mutex
	sessions  = map[string]*agentBox{}
)

func box(name string) *agentBox {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	b := sessions[name]
	if b == nil {
		b = &agentBox{waitReply: map[string]chan []byte{}}
		sessions[name] = b
	}
	return b
}

// PushAgent delivers one JSON-RPC request to the agent's open connection.
// reply is set when the caller waits for POST /a2a/connect. A nil reply is fire-and-forget.
func PushAgent(name string, body []byte, reply chan []byte) {
	if name == "" || len(body) == 0 {
		return
	}
	b := box(name)
	b.mu.Lock()
	defer b.mu.Unlock()
	id := rpcID(body)
	if reply != nil && id != "" {
		b.waitReply[id] = reply
	}
	if b.waiter != nil {
		w := b.waiter
		b.waiter = nil
		w <- append([]byte(nil), body...)
		return
	}
	if len(b.q) >= agentQueueMax {
		dropped := b.q[0]
		b.q = b.q[1:]
		if dropped.reply != nil && dropped.id != "" {
			delete(b.waitReply, dropped.id)
		}
	}
	b.q = append(b.q, queuedCall{id: id, body: append([]byte(nil), body...), reply: reply})
}

// TakeAgent waits for the next request addressed to this agent.
// The second result is false on timeout or cancel, and busy is true when another connection is already waiting.
func TakeAgent(ctx context.Context, name string) (body []byte, busy bool) {
	b := box(name)
	b.mu.Lock()
	if len(b.q) > 0 {
		item := b.q[0]
		b.q = b.q[1:]
		b.mu.Unlock()
		return item.body, false
	}
	if b.waiter != nil {
		b.mu.Unlock()
		return nil, true
	}
	ch := make(chan []byte, 1)
	b.waiter = ch
	b.mu.Unlock()
	select {
	case body = <-ch:
		return body, false
	case <-ctx.Done():
		b.mu.Lock()
		if b.waiter == ch {
			b.waiter = nil
		}
		select {
		case late := <-ch:
			b.q = append([]queuedCall{{id: rpcID(late), body: late}}, b.q...)
		default:
		}
		b.mu.Unlock()
		return nil, false
	}
}

// ReplyAgent completes the caller that is waiting on this JSON-RPC id.
func ReplyAgent(name string, body []byte) bool {
	id := rpcID(body)
	if name == "" || id == "" {
		return false
	}
	b := box(name)
	b.mu.Lock()
	ch := b.waitReply[id]
	delete(b.waitReply, id)
	b.mu.Unlock()
	if ch == nil {
		return false
	}
	select {
	case ch <- append([]byte(nil), body...):
	default:
	}
	return true
}

// CancelAgent drops a still-queued call and its reply waiter.
func CancelAgent(name, id string) {
	if name == "" || id == "" {
		return
	}
	b := box(name)
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.waitReply, id)
	kept := b.q[:0]
	for _, item := range b.q {
		if item.reply != nil && item.id == id {
			continue
		}
		kept = append(kept, item)
	}
	b.q = kept
}

func rpcID(body []byte) string {
	var m struct {
		ID json.RawMessage `json:"id"`
	}
	if json.Unmarshal(body, &m) != nil || len(m.ID) == 0 || string(m.ID) == "null" {
		return ""
	}
	return string(m.ID)
}
