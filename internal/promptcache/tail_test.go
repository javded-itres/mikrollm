package promptcache

import (
	"bytes"
	"strings"
	"testing"
)

func TestScannerSSECommentsDoneAndUsage(t *testing.T) {
	var s Scanner
	s.Feed([]byte(": OPENROUTER PROCESSING\n"))
	s.Feed([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}]}\n\n"))
	s.Feed([]byte("data: {\"usage\":{\"prompt_tokens\":10,\"prompt_tokens_details\":{\"cached_tokens\":8}},\"cache_discount\":0.3}\n"))
	s.Feed([]byte("data: [DONE]\n"))
	u := ParseJSON(s.LastUsage())
	if u.CachedTokens != 8 || !u.HasDiscount || u.CacheDiscount != 0.3 {
		t.Fatalf("%+v %s", u, s.LastUsage())
	}
}

func TestScannerOverflowKeepsPrevious(t *testing.T) {
	var s Scanner
	s.Feed([]byte("data: {\"usage\":{\"prompt_tokens\":1,\"prompt_tokens_details\":{\"cached_tokens\":7}}}\n"))
	big := bytes.Repeat([]byte("x"), TailMax+10)
	s.Feed(big)
	u := ParseJSON(s.LastUsage())
	if u.CachedTokens != 7 {
		t.Fatalf("lost previous %+v", u)
	}
}

func TestScannerHugeUsageFrameDropped(t *testing.T) {
	var s Scanner
	payload := `{"usage":{"prompt_tokens":1},"pad":"` + strings.Repeat("a", TailMax) + `"}`
	s.Feed([]byte("data: " + payload + "\n"))
	if len(s.LastUsage()) != 0 {
		t.Fatal("should drop huge frame")
	}
}

func TestScannerNDJSON(t *testing.T) {
	var s Scanner
	s.Feed([]byte("{\"message\":{\"content\":\"a\"}}\n"))
	s.Feed([]byte("{\"done\":true,\"prompt_eval_count\":4,\"eval_count\":2}\n"))
	u := ParseJSON(s.LastUsage())
	if u.PromptTokens != 4 || u.CompletionTokens != 2 {
		t.Fatalf("%+v", u)
	}
}
