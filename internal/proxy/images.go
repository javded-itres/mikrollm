package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/javded-itres/mikrollm/internal/domain"
)

var videoModels sync.Map // video id -> gateway model alias

func (p *Proxy) ImagesGenerations(w http.ResponseWriter, r *http.Request) {
	p.serveImages(w, r, true)
}

func (p *Proxy) ServeImages(w http.ResponseWriter, r *http.Request) {
	p.serveImages(w, r, false)
}

func (p *Proxy) VideosCreate(w http.ResponseWriter, r *http.Request) {
	p.serveVideosCreate(w, r, true)
}

func (p *Proxy) ServeVideos(w http.ResponseWriter, r *http.Request) {
	p.serveVideosCreate(w, r, false)
}

func (p *Proxy) VideosGet(w http.ResponseWriter, r *http.Request) {
	p.serveVideosGet(w, r, true, r.PathValue("id"), false)
}

func (p *Proxy) VideosContent(w http.ResponseWriter, r *http.Request) {
	p.serveVideosGet(w, r, true, r.PathValue("id"), true)
}

func (p *Proxy) ServeVideoStatus(w http.ResponseWriter, r *http.Request) {
	p.serveVideosGet(w, r, false, r.PathValue("id"), false)
}

func (p *Proxy) ServeVideoContent(w http.ResponseWriter, r *http.Request) {
	p.serveVideosGet(w, r, false, r.PathValue("id"), true)
}

func (p *Proxy) serveImages(w http.ResponseWriter, r *http.Request, needKey bool) {
	body, model, err := readModelBody(r)
	if err != nil || model == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "model and prompt required"}})
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	b, _, perr := p.pick(model)
	if perr == nil && b.KindNorm() == domain.KindOpenRouter {
		r.Body = io.NopCloser(bytes.NewReader(imageJSONToChat(body)))
		rec := &memWriter{h: http.Header{}}
		p.forward(rec, r, "/v1/chat/completions", needKey)
		raw := rec.buf.Bytes()
		mapped, ok := chatToImageResponse(raw)
		if ok && (rec.code == 0 || rec.code < 400) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(mapped)
			return
		}
		if msg, filtered := imageChatRefusal(raw); filtered {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": map[string]any{
					"message": msg,
					"type":    "content_filter",
					"code":    "content_filter",
					"param":   "prompt",
				},
			})
			return
		}
		copySafeHeaders(w.Header(), rec.h)
		code := rec.code
		if code == 0 {
			code = http.StatusBadGateway
		}
		w.WriteHeader(code)
		_, _ = w.Write(raw)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(foldImagePrompt(body)))
	p.forward(w, r, "/v1/images/generations", needKey)
}

func (p *Proxy) serveVideosCreate(w http.ResponseWriter, r *http.Request, needKey bool) {
	k, ok := p.mediaKey(w, r, needKey)
	if !ok {
		return
	}
	body, model, err := readModelBody(r)
	if err != nil || model == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "model and prompt required"}})
		return
	}
	rec := &memWriter{h: http.Header{}}
	_, _, _, _ = p.Forward(r.Context(), rec, k, "/v1/videos", foldImagePrompt(body), model)
	if rec.code < 400 {
		if id := videoIDFrom(rec.buf.Bytes()); id != "" {
			videoModels.Store(id, model)
		}
	}
	copySafeHeaders(w.Header(), rec.h)
	code := rec.code
	if code == 0 {
		code = 200
	}
	w.WriteHeader(code)
	_, _ = w.Write(rec.buf.Bytes())
}

func (p *Proxy) serveVideosGet(w http.ResponseWriter, r *http.Request, needKey bool, id string, content bool) {
	k, ok := p.mediaKey(w, r, needKey)
	if !ok {
		return
	}
	model := strings.TrimSpace(r.URL.Query().Get("model"))
	if model == "" {
		if v, ok := videoModels.Load(id); ok {
			model, _ = v.(string)
		}
	}
	if model == "" || id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "model query or known video id required"}})
		return
	}
	path := "/v1/videos/" + id
	if content {
		path += "/content"
	}
	body, _ := json.Marshal(map[string]string{"model": model})
	_, _, _, _ = p.Forward(r.Context(), w, k, path, body, model)
}

func (p *Proxy) mediaKey(w http.ResponseWriter, r *http.Request, needKey bool) (domain.APIKey, bool) {
	if needKey {
		return p.requireKey(w, r)
	}
	return domain.APIKey{Name: "admin", Prefix: "admin", AllowedModels: []string{"*"}, Enabled: true}, true
}

func readModelBody(r *http.Request) ([]byte, string, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if err != nil {
		return nil, "", err
	}
	var mb modelBody
	_ = json.Unmarshal(body, &mb)
	return body, mb.Model, nil
}

func imageJSONToChat(body []byte) []byte {
	var req struct {
		Model    string `json:"model"`
		Prompt   string `json:"prompt"`
		Size     string `json:"size"`
		Messages []any  `json:"messages"`
	}
	if json.Unmarshal(body, &req) != nil {
		return body
	}
	msgs := sanitizeImageMessages(req.Messages)
	if len(msgs) == 0 {
		if strings.TrimSpace(req.Prompt) == "" {
			return body
		}
		content := req.Prompt
		if req.Size != "" {
			content = content + "\nImage size: " + req.Size
		}
		msgs = []any{map[string]any{"role": "user", "content": content}}
	} else if req.Size != "" {
		msgs = appendSizeHint(msgs, req.Size)
	}
	if !firstIsSystem(msgs) {
		sys := map[string]any{
			"role":    "system",
			"content": "Continue the same image generation. Keep the original subject and scene unless the user explicitly changes them. Treat each new user message as a revision of the previous image. If a previous image is in the thread, edit that image.",
		}
		msgs = append([]any{sys}, msgs...)
	}
	out, err := json.Marshal(map[string]any{
		"model": req.Model, "modalities": []string{"image", "text"}, "messages": msgs,
	})
	if err != nil {
		return body
	}
	return out
}

func foldImagePrompt(body []byte) []byte {
	var raw map[string]any
	if json.Unmarshal(body, &raw) != nil {
		return body
	}
	msgs, _ := raw["messages"].([]any)
	if len(msgs) == 0 {
		return body
	}
	var b strings.Builder
	b.WriteString("Original image task and subsequent revisions. Produce one image matching the latest state.\n\n")
	for _, item := range msgs {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role, _ := m["role"].(string)
		text := mediaContentText(m["content"])
		if text == "" {
			continue
		}
		switch strings.ToLower(role) {
		case "system":
			continue
		case "assistant":
			b.WriteString("Assistant: ")
		default:
			b.WriteString("User: ")
		}
		b.WriteString(text)
		b.WriteByte('\n')
	}
	raw["prompt"] = strings.TrimSpace(b.String())
	out, err := json.Marshal(raw)
	if err != nil {
		return body
	}
	return out
}

func sanitizeImageMessages(in []any) []any {
	var out []any
	for _, item := range in {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role, _ := m["role"].(string)
		if role == "" {
			role = "user"
		}
		out = append(out, map[string]any{"role": role, "content": m["content"]})
	}
	return out
}

func firstIsSystem(msgs []any) bool {
	if len(msgs) == 0 {
		return false
	}
	m, ok := msgs[0].(map[string]any)
	if !ok {
		return false
	}
	role, _ := m["role"].(string)
	return strings.EqualFold(role, "system")
}

func appendSizeHint(msgs []any, size string) []any {
	last, ok := msgs[len(msgs)-1].(map[string]any)
	if !ok {
		return msgs
	}
	if s, ok := last["content"].(string); ok {
		last["content"] = s + "\nImage size: " + size
		msgs[len(msgs)-1] = last
	}
	return msgs
}

func mediaContentText(content any) string {
	switch t := content.(type) {
	case string:
		return t
	case []any:
		var b strings.Builder
		for _, p := range t {
			part, ok := p.(map[string]any)
			if !ok {
				continue
			}
			if s, _ := part["text"].(string); s != "" {
				if b.Len() > 0 {
					b.WriteByte(' ')
				}
				b.WriteString(s)
			}
		}
		return b.String()
	default:
		return ""
	}
}

func imageChatRefusal(raw []byte) (string, bool) {
	var payload struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
		Choices []struct {
			FinishReason       string `json:"finish_reason"`
			NativeFinishReason string `json:"native_finish_reason"`
		} `json:"choices"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return "", false
	}
	if strings.EqualFold(payload.Error.Type, "content_filter") || strings.Contains(strings.ToLower(payload.Error.Message), "content_filter") {
		msg := strings.TrimSpace(payload.Error.Message)
		if msg == "" {
			msg = "Провайдер отклонил запрос фильтром контента."
		}
		return msg, true
	}
	for _, ch := range payload.Choices {
		fr := strings.ToLower(ch.FinishReason)
		nfr := strings.ToUpper(ch.NativeFinishReason)
		if fr == "content_filter" || nfr == "PROHIBITED_CONTENT" || nfr == "SAFETY" {
			return "Провайдер отклонил запрос фильтром контента (nudity/safety). Gemini Image такое не рисует — возьмите модель без этого фильтра (Flux, Grok Imagine и т.п.).", true
		}
	}
	return "", false
}

func chatToImageResponse(raw []byte) ([]byte, bool) {
	var payload struct {
		Created int64 `json:"created"`
		Choices []struct {
			Message struct {
				Images []struct {
					ImageURL struct {
						URL string `json:"url"`
					} `json:"image_url"`
					URL string `json:"url"`
				} `json:"images"`
				Content any `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Data []struct {
			URL     string `json:"url"`
			B64JSON string `json:"b64_json"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return nil, false
	}
	if len(payload.Data) > 0 {
		return raw, true
	}
	var data []imgPart
	for _, ch := range payload.Choices {
		for _, im := range ch.Message.Images {
			u := im.ImageURL.URL
			if u == "" {
				u = im.URL
			}
			data = append(data, splitImageURL(u))
		}
		data = append(data, imagesFromContent(ch.Message.Content)...)
	}
	if len(data) == 0 {
		return nil, false
	}
	created := payload.Created
	if created == 0 {
		created = time.Now().Unix()
	}
	out, err := json.Marshal(map[string]any{"created": created, "data": data})
	if err != nil {
		return nil, false
	}
	return out, true
}

type imgPart struct {
	URL     string `json:"url,omitempty"`
	B64JSON string `json:"b64_json,omitempty"`
}

func splitImageURL(u string) imgPart {
	if strings.HasPrefix(u, "data:") {
		if i := strings.Index(u, ","); i >= 0 {
			return imgPart{B64JSON: u[i+1:]}
		}
	}
	return imgPart{URL: u}
}

func imagesFromContent(content any) []imgPart {
	var out []imgPart
	arr, ok := content.([]any)
	if !ok {
		return out
	}
	for _, p := range arr {
		m, ok := p.(map[string]any)
		if !ok {
			continue
		}
		if iu, ok := m["image_url"].(map[string]any); ok {
			if u, _ := iu["url"].(string); u != "" {
				out = append(out, splitImageURL(u))
			}
		}
	}
	return out
}

func videoIDFrom(raw []byte) string {
	var v struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &v)
	return v.ID
}
