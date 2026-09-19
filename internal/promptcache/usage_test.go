package promptcache

import "testing"

func TestParseJSONOpenRouter(t *testing.T) {
	u := ParseJSON([]byte(`{"usage":{"prompt_tokens":10339,"completion_tokens":60,"cost":0.042,"prompt_tokens_details":{"cached_tokens":10318,"cache_write_tokens":0}},"cache_discount":1.2}`))
	if u.PromptTokens != 10339 || u.CachedTokens != 10318 || !u.HasCost || u.Cost != 0.042 {
		t.Fatalf("%+v", u)
	}
	if !u.HasDiscount || u.CacheDiscount != 1.2 {
		t.Fatalf("discount %+v", u)
	}
}

func TestParseJSONUsageDiscount(t *testing.T) {
	u := ParseJSON([]byte(`{"usage":{"prompt_tokens":10,"cache_discount":-0.5,"cost":1}}`))
	if !u.HasDiscount || u.CacheDiscount != -0.5 || !u.HasCost {
		t.Fatalf("%+v", u)
	}
}

func TestParseJSONAnthropicNative(t *testing.T) {
	u := ParseJSON([]byte(`{"usage":{"input_tokens":100,"output_tokens":5,"cache_read_input_tokens":80,"cache_creation_input_tokens":20}}`))
	if u.PromptTokens != 100 || u.CompletionTokens != 5 || u.CachedTokens != 80 || u.CacheWriteTokens != 20 {
		t.Fatalf("%+v", u)
	}
}

func TestParseJSONOllama(t *testing.T) {
	u := ParseJSON([]byte(`{"done":true,"prompt_eval_count":12,"eval_count":3}`))
	if u.PromptTokens != 12 || u.CompletionTokens != 3 || u.CachedTokens != 0 || u.HasDiscount {
		t.Fatalf("%+v", u)
	}
}

func TestParseJSONNegativeTokens(t *testing.T) {
	u := ParseJSON([]byte(`{"usage":{"prompt_tokens":-5,"prompt_tokens_details":{"cached_tokens":-1}}}`))
	if u.PromptTokens != 0 || u.CachedTokens != 0 {
		t.Fatalf("%+v", u)
	}
}

func TestParseUsageMapNoRootDiscount(t *testing.T) {
	u := ParseUsageMap(map[string]any{"prompt_tokens": 1, "cost": 2})
	if u.HasDiscount || !u.HasCost || u.Cost != 2 {
		t.Fatalf("%+v", u)
	}
}
