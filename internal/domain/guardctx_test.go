package domain

import (
	"context"
	"strings"
	"testing"
)

func TestWithSessionIDTrimCapCRLF(t *testing.T) {
	if SessionIDFrom(nil) != "" {
		t.Fatal("nil ctx")
	}
	ctx := WithSessionID(nil, "  abc  ")
	if SessionIDFrom(ctx) != "abc" {
		t.Fatalf("trim %q", SessionIDFrom(ctx))
	}
	ctx = WithSessionID(context.Background(), "one\r\ninject")
	if SessionIDFrom(ctx) != "one" {
		t.Fatalf("crlf %q", SessionIDFrom(ctx))
	}
	ctx = WithSessionID(context.Background(), "   \n")
	if SessionIDFrom(ctx) != "" {
		t.Fatalf("empty after strip %q", SessionIDFrom(ctx))
	}
	long := strings.Repeat("x", 300)
	ctx = WithSessionID(context.Background(), long)
	got := SessionIDFrom(ctx)
	if len(got) != 256 || got != long[:256] {
		t.Fatalf("cap len=%d", len(got))
	}
}

func TestWithSessionIDPreservesQueueAlias(t *testing.T) {
	ctx := WithQueueAlias(context.Background(), "coder")
	ctx = WithSessionID(ctx, "sess-1")
	if QueueAliasFrom(ctx) != "coder" || SessionIDFrom(ctx) != "sess-1" {
		t.Fatalf("alias=%q session=%q", QueueAliasFrom(ctx), SessionIDFrom(ctx))
	}
}
