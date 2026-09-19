package domain

import (
	"context"
	"strings"
)

type guardCtxKey int

const (
	queueAliasCtxKey guardCtxKey = 1
	sessionIDCtxKey  guardCtxKey = 2
	sessionIDMaxLen              = 256
)

func WithQueueAlias(ctx context.Context, alias string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return ctx
	}
	return context.WithValue(ctx, queueAliasCtxKey, alias)
}

func QueueAliasFrom(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	s, _ := ctx.Value(queueAliasCtxKey).(string)
	return s
}

func WithSessionID(ctx context.Context, id string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	id = strings.TrimSpace(id)
	if i := strings.IndexAny(id, "\r\n"); i >= 0 {
		id = strings.TrimSpace(id[:i])
	}
	if id == "" {
		return ctx
	}
	if len(id) > sessionIDMaxLen {
		id = id[:sessionIDMaxLen]
	}
	return context.WithValue(ctx, sessionIDCtxKey, id)
}

func SessionIDFrom(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	s, _ := ctx.Value(sessionIDCtxKey).(string)
	return s
}
