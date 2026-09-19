package proxy

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/javded-itres/mikrollm/internal/domain"
)

func TestChatToImageResponse(t *testing.T) {
	raw := []byte(`{"choices":[{"message":{"images":[{"image_url":{"url":"data:image/png;base64,QUJD"}}]}}]}`)
	out, ok := chatToImageResponse(raw)
	if !ok {
		t.Fatal("map")
	}
	var got struct {
		Data []struct {
			B64JSON string `json:"b64_json"`
		} `json:"data"`
	}
	if json.Unmarshal(out, &got) != nil || len(got.Data) != 1 || got.Data[0].B64JSON != "QUJD" {
		t.Fatalf("%s", out)
	}
}

func TestImageChatRefusal(t *testing.T) {
	raw := []byte(`{"choices":[{"finish_reason":"content_filter","native_finish_reason":"PROHIBITED_CONTENT","message":{"content":null}}]}`)
	msg, ok := imageChatRefusal(raw)
	if !ok || msg == "" {
		t.Fatalf("want filter: %q %v", msg, ok)
	}
}

func TestImageJSONToChat(t *testing.T) {
	out := imageJSONToChat([]byte(`{"model":"flux","prompt":"a cat","size":"1024x1024"}`))
	if !strings.Contains(string(out), `"modalities"`) || !strings.Contains(string(out), "a cat") {
		t.Fatalf("%s", out)
	}
}

func TestImageJSONToChatKeepsHistory(t *testing.T) {
	body := `{"model":"m","prompt":"в платье","messages":[{"role":"user","content":"девушка на берегу"},{"role":"assistant","content":"blocked"},{"role":"user","content":"в платье"}]}`
	out := string(imageJSONToChat([]byte(body)))
	if !strings.Contains(out, "девушка на берегу") || !strings.Contains(out, "в платье") {
		t.Fatalf("%s", out)
	}
	if !strings.Contains(out, "Continue the same image") {
		t.Fatal("system")
	}
}

func TestFoldImagePrompt(t *testing.T) {
	body := []byte(`{"model":"m","prompt":"x","messages":[{"role":"user","content":"original"},{"role":"user","content":"revision"}]}`)
	out := string(foldImagePrompt(body))
	if !strings.Contains(out, "original") || !strings.Contains(out, "revision") {
		t.Fatalf("%s", out)
	}
}

func TestRewriteImagePath(t *testing.T) {
	or := domain.Backend{Kind: "openrouter"}
	if rewriteUpstreamPath(or, "/v1/images/generations") != "/images/generations" {
		t.Fatal("openrouter images")
	}
	ollama := domain.Backend{Kind: "ollama"}
	if rewriteUpstreamPath(ollama, "/v1/images/generations") != "/v1/images/generations" {
		t.Fatal("ollama images")
	}
	if rewriteUpstreamPath(or, "/v1/videos/abc") != "/videos/abc" {
		t.Fatal("openrouter videos")
	}
}
