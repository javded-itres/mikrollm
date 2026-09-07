package domain

import "testing"

func TestAPIKeyAllows(t *testing.T) {
	if !(APIKey{}).Allows("qwen") {
		t.Fatal("empty allowlist must allow")
	}
	k := APIKey{AllowedModels: []string{"qwen"}}
	if !k.Allows("qwen") || k.Allows("other") {
		t.Fatal(k)
	}
	star := APIKey{AllowedModels: []string{"*"}}
	if !star.Allows("anything") {
		t.Fatal("star")
	}
}
