package proxy

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/javded-itres/mikrollm/internal/domain"
	"github.com/javded-itres/mikrollm/internal/store"
)

// 1×1 PNG (valid, 67 bytes).
var png1x1 = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89,
	0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41, 0x54,
	0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05, 0x00, 0x01,
	0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00,
	0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}

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

func TestOpenRouterImageBody(t *testing.T) {
	out := string(openRouterImageBody([]byte(`{"model":"flux","prompt":"a cat","size":"1024x1024"}`)))
	if strings.Contains(out, `"modalities"`) || strings.Contains(out, "/chat") {
		t.Fatalf("%s", out)
	}
	if !strings.Contains(out, `"prompt":"a cat"`) || !strings.Contains(out, "flux") {
		t.Fatalf("%s", out)
	}
	hist := `{"model":"flux","prompt":"edit","messages":[{"role":"assistant","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,QUJD"}}]}]}`
	got := string(openRouterImageBody([]byte(hist)))
	if !strings.Contains(got, `"input_references"`) || strings.Contains(got, `"messages"`) {
		t.Fatalf("%s", got)
	}
	both := `{"model":"flux","prompt":"mix","input_references":[{"type":"image_url","image_url":{"url":"data:image/jpeg;base64,AAA="}}],"messages":[{"role":"assistant","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,QUJD"}}]}]}`
	merged := string(openRouterImageBody([]byte(both)))
	if !strings.Contains(merged, "AAA=") || !strings.Contains(merged, "QUJD") {
		t.Fatalf("merge %s", merged)
	}
}

func TestOpenComfyMediaBodyMapsRefs(t *testing.T) {
	body := []byte(`{"model":"flux","prompt":"x","input_references":[{"type":"image_url","image_url":{"url":"data:image/png;base64,QUJD"}}],"messages":[{"role":"user","content":"hi"}]}`)
	got := string(openComfyMediaBody(body))
	if !strings.Contains(got, `"input_image":"data:image/png;base64,QUJD"`) {
		t.Fatalf("input_image missing: %s", got)
	}
	if strings.Contains(got, `"messages"`) {
		t.Fatalf("messages leaked: %s", got)
	}
	if !strings.Contains(got, `"prompt":"hi"`) {
		t.Fatalf("last user prompt %s", got)
	}
}

func TestOpenComfyMediaBodyDropsAssistantErrors(t *testing.T) {
	body := []byte(`{"model":"z","prompt":"x","messages":[{"role":"user","content":"яблоко"},{"role":"assistant","content":"prompt 400: {\"error\":{\"details\":\"clip_name\"}}"},{"role":"user","content":"гнилое яблоко"}]}`)
	got := string(openComfyMediaBody(body))
	if strings.Contains(got, "Original image task") || strings.Contains(got, "prompt 400") || strings.Contains(got, "clip_name") {
		t.Fatalf("assistant dump leaked: %s", got)
	}
	var raw map[string]any
	if json.Unmarshal([]byte(got), &raw) != nil {
		t.Fatal(got)
	}
	if raw["prompt"] != "гнилое яблоко" {
		t.Fatalf("prompt=%v", raw["prompt"])
	}
}

func TestFoldImagePromptKeepsRefs(t *testing.T) {
	body := []byte(`{"model":"m","prompt":"x","input_references":[{"type":"image_url","image_url":{"url":"data:image/png;base64,QUJD"}}],"messages":[{"role":"user","content":"original"}]}`)
	out := string(foldImagePrompt(body))
	if !strings.Contains(out, "input_references") || !strings.Contains(out, "QUJD") {
		t.Fatalf("%s", out)
	}
}

func TestFoldImagePrompt(t *testing.T) {
	body := []byte(`{"model":"m","prompt":"x","messages":[{"role":"user","content":"original"},{"role":"user","content":"revision"}]}`)
	out := string(foldImagePrompt(body))
	if !strings.Contains(out, "original") || !strings.Contains(out, "revision") {
		t.Fatalf("%s", out)
	}
}

func TestImagesHubRelay(t *testing.T) {
	st, _, _, px, _ := setup(t)
	var gotPath string
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"QUJD"}]}`))
	}))
	t.Cleanup(hub.Close)
	px.SetHubDial(fakeHubDial{url: hub.URL})
	if _, err := st.SaveModel(store.Model{
		Alias: "gemini-hub", UpstreamName: "google/gemini-2.5-flash-image", Enabled: true,
		HubNodeID: "npeer", HubNodeName: "ams-1",
	}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{"model":"gemini-hub","prompt":"cat"}`))
	rec := httptest.NewRecorder()
	px.ServeImages(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "QUJD") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if gotPath != "/v1/relay/npeer/images" {
		t.Fatalf("path %s", gotPath)
	}
}

func TestVideosHubRelay(t *testing.T) {
	st, _, _, px, _ := setup(t)
	var gotPath string
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/content") {
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("mp4"))
			return
		}
		_, _ = w.Write([]byte(`{"id":"vid_1","status":"completed"}`))
	}))
	t.Cleanup(hub.Close)
	px.SetHubDial(fakeHubDial{url: hub.URL})
	if _, err := st.SaveModel(store.Model{
		Alias: "seedance-hub", UpstreamName: "bytedance/seedance-2.0-mini", Enabled: true,
		HubNodeID: "npeer", HubNodeName: "ams-1",
	}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{"model":"seedance-hub","prompt":"cat walks"}`))
	rec := httptest.NewRecorder()
	px.ServeVideos(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "vid_1") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if gotPath != "/v1/relay/npeer/videos" {
		t.Fatalf("create path %s", gotPath)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/videos/{id}", px.ServeVideoStatus)
	mux.HandleFunc("GET /v1/videos/{id}/content", px.ServeVideoContent)
	get := httptest.NewRequest(http.MethodGet, "/v1/videos/vid_1?model=seedance-hub", nil)
	grec := httptest.NewRecorder()
	mux.ServeHTTP(grec, get)
	if grec.Code != 200 || gotPath != "/v1/relay/npeer/videos/vid_1" {
		t.Fatalf("get %d %s path %s", grec.Code, grec.Body.String(), gotPath)
	}
	cget := httptest.NewRequest(http.MethodGet, "/v1/videos/vid_1/content?model=seedance-hub", nil)
	crec := httptest.NewRecorder()
	mux.ServeHTTP(crec, cget)
	if crec.Code != 200 || crec.Body.String() != "mp4" || gotPath != "/v1/relay/npeer/videos/vid_1/content" {
		t.Fatalf("content %d %q path %s", crec.Code, crec.Body.String(), gotPath)
	}
}

func TestRewriteImagePath(t *testing.T) {
	or := domain.Backend{Kind: "openrouter"}
	if rewriteUpstreamPath(or, "/v1/images/generations") != "/images" {
		t.Fatal("openrouter images")
	}
	ollama := domain.Backend{Kind: "ollama"}
	if rewriteUpstreamPath(ollama, "/v1/images/generations") != "/v1/images/generations" {
		t.Fatal("ollama images")
	}
	if rewriteUpstreamPath(or, "/v1/videos/abc") != "/videos/abc" {
		t.Fatal("openrouter videos")
	}
	oc := domain.Backend{Kind: "opencomfy"}
	if rewriteUpstreamPath(oc, "/v1/images/generations") != "/v1/images/generations" {
		t.Fatal("opencomfy images")
	}
	if rewriteUpstreamPath(oc, "/v1/videos/abc") != "/v1/videos/abc" {
		t.Fatal("opencomfy videos")
	}
	if rewriteUpstreamPath(oc, "/v1/videos/abc/content") != "/v1/videos/abc/content" {
		t.Fatal("opencomfy video content")
	}
}

func TestOpenRouterServeImagesUsesNativeAPI(t *testing.T) {
	st, h, _, px, _ := setup(t)
	var gotPath, gotBody string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/key" || r.URL.Path == "/models" || r.URL.Path == "/images/models" || r.URL.Path == "/videos/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"black-forest-labs/flux.2-klein-4b"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/images":
			gotPath = r.URL.Path
			b, _ := io.ReadAll(r.Body)
			gotBody = string(b)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"b64_json":"QUJD"}]}`))
		case strings.Contains(r.URL.Path, "chat"):
			http.Error(w, `{"error":{"message":"No endpoints found that support the requested output modalities: image, text"}}`, http.StatusNotFound)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(up.Close)
	bid, err := st.UpsertBackend("or", up.URL, true, 1, "openrouter", "sk-or-test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveModel(store.Model{
		Alias: "flux", UpstreamName: "black-forest-labs/flux.2-klein-4b", Enabled: true, BackendIDs: []int64{bid},
	}); err != nil {
		t.Fatal(err)
	}
	h.CheckOnce()
	time.Sleep(10 * time.Millisecond)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{"model":"flux","prompt":"a cat"}`))
	rec := httptest.NewRecorder()
	px.ServeImages(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "QUJD") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if gotPath != "/images" {
		t.Fatalf("path %s", gotPath)
	}
	if strings.Contains(gotBody, "modalities") {
		t.Fatalf("chat shim leaked: %s", gotBody)
	}
}

func TestSameOriginHTTP(t *testing.T) {
	base := "http://192.168.88.252:8788"
	if !sameOriginHTTP(base, "http://192.168.88.252:8788/v1/files/file_1?exp=&sig=x") {
		t.Fatal("same host")
	}
	if sameOriginHTTP(base, "http://evil.example/steal") {
		t.Fatal("foreign host")
	}
	if sameOriginHTTP(base, "https://192.168.88.252:8788/v1/files/x") {
		t.Fatal("scheme mismatch")
	}
	if sameOriginHTTP(base, "data:image/png;base64,QUJD") {
		t.Fatal("data url")
	}
}

func TestLooksLikeImage(t *testing.T) {
	if !looksLikeImage(png1x1) {
		t.Fatal("png")
	}
	if looksLikeImage([]byte(`{"error":"nope"}`)) {
		t.Fatal("json")
	}
	if looksLikeImage([]byte{0xff, 0xd8, 0xff, 0xe0}) {
		// jpeg magic is enough
	} else {
		t.Fatal("jpeg")
	}
}

func TestOpenComfyImagesInlineB64(t *testing.T) {
	st, h, _, px, _ := setup(t)
	var fileHits int
	var sawAuth string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/health":
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/v1/models" || r.URL.Path == "/v1/images/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"toy-image"}]}`))
		case r.URL.Path == "/v1/videos/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/images/generations":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"created":1,"data":[{"url":"http://` + r.Host + `/v1/files/file_1?exp=&sig=abc"}]}`))
		case r.URL.Path == "/v1/files/file_1":
			fileHits++
			sawAuth = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(png1x1)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(up.Close)
	bid, err := st.UpsertBackend("comfy", up.URL, true, 1, "opencomfy", "sk-test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveModel(store.Model{
		Alias: "toy-image", UpstreamName: "toy-image", Enabled: true, BackendIDs: []int64{bid},
	}); err != nil {
		t.Fatal(err)
	}
	h.CheckOnce()
	time.Sleep(10 * time.Millisecond)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{"model":"toy-image","prompt":"a cube"}`))
	rec := httptest.NewRecorder()
	px.ServeImages(rec, req)
	if rec.Code != 200 {
		t.Fatalf("code %d %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Data []struct {
			URL     string `json:"url"`
			B64JSON string `json:"b64_json"`
		} `json:"data"`
	}
	if json.Unmarshal(rec.Body.Bytes(), &got) != nil || len(got.Data) != 1 {
		t.Fatalf("%s", rec.Body.String())
	}
	if got.Data[0].URL != "" {
		t.Fatalf("url should be stripped: %s", rec.Body.String())
	}
	want := base64.StdEncoding.EncodeToString(png1x1)
	if got.Data[0].B64JSON != want {
		t.Fatalf("b64 %q", got.Data[0].B64JSON)
	}
	if fileHits != 1 {
		t.Fatalf("file hits %d", fileHits)
	}
	if sawAuth != "Bearer sk-test" {
		t.Fatalf("auth %q", sawAuth)
	}
}

func TestOpenComfyServeImagesMapsInputReferences(t *testing.T) {
	st, h, _, px, _ := setup(t)
	var gotBody string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/health":
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/v1/models" || r.URL.Path == "/v1/images/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"toy-image","parameters":[{"name":"prompt","type":"string","required":true},{"name":"input_image","type":"image","required":true}],"required_parameters":["prompt","input_image"]}]}`))
		case r.URL.Path == "/v1/videos/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[]}`))
		case r.URL.Path == "/v1/models/toy-image":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"toy-image","parameters":[{"name":"prompt","type":"string","required":true},{"name":"input_image","type":"image","required":true}],"required_parameters":["prompt","input_image"],"mcp_tool":"generate_toy-image"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/images/generations":
			b, _ := io.ReadAll(r.Body)
			gotBody = string(b)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"created":1,"data":[{"b64_json":"QUJD"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(up.Close)
	bid, err := st.UpsertBackend("comfy", up.URL, true, 1, "opencomfy", "sk-test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveModel(store.Model{
		Alias: "toy-image", UpstreamName: "toy-image", Enabled: true, BackendIDs: []int64{bid}, Media: []string{"image"},
	}); err != nil {
		t.Fatal(err)
	}
	h.CheckOnce()
	time.Sleep(10 * time.Millisecond)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{"model":"toy-image","prompt":"a cube","input_references":[{"type":"image_url","image_url":{"url":"data:image/png;base64,QUJD"}}]}`))
	rec := httptest.NewRecorder()
	px.ServeImages(rec, req)
	if rec.Code != 200 {
		t.Fatalf("code %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(gotBody, `"input_image"`) || !strings.Contains(gotBody, "QUJD") {
		t.Fatalf("upstream body %s", gotBody)
	}

	prec := httptest.NewRecorder()
	preq := httptest.NewRequest(http.MethodGet, "/admin/model-params?model=toy-image", nil)
	px.ServeModelParams(prec, preq)
	if prec.Code != 200 {
		t.Fatalf("params %d %s", prec.Code, prec.Body.String())
	}
	if !strings.Contains(prec.Body.String(), "input_image") || !strings.Contains(prec.Body.String(), "required") {
		t.Fatalf("params body %s", prec.Body.String())
	}
}

func TestImagesInlineSkipsForeignURL(t *testing.T) {
	st, h, _, px, _ := setup(t)
	var evilHits int
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		evilHits++
		_, _ = w.Write(png1x1)
	}))
	t.Cleanup(evil.Close)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/health":
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/v1/models" || r.URL.Path == "/v1/images/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"toy-image"}]}`))
		case r.URL.Path == "/v1/videos/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/images/generations":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"created":1,"data":[{"url":"` + evil.URL + `/secret.png"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(up.Close)
	bid, err := st.UpsertBackend("comfy", up.URL, true, 1, "opencomfy", "sk-test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveModel(store.Model{
		Alias: "toy-image", UpstreamName: "toy-image", Enabled: true, BackendIDs: []int64{bid},
	}); err != nil {
		t.Fatal(err)
	}
	h.CheckOnce()
	time.Sleep(10 * time.Millisecond)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{"model":"toy-image","prompt":"cat"}`))
	rec := httptest.NewRecorder()
	px.ServeImages(rec, req)
	if rec.Code != 200 {
		t.Fatalf("code %d %s", rec.Code, rec.Body.String())
	}
	if evilHits != 0 {
		t.Fatalf("ssrf hits %d", evilHits)
	}
	if !strings.Contains(rec.Body.String(), evil.URL) {
		t.Fatalf("kept url: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"b64_json"`) {
		t.Fatalf("unexpected b64: %s", rec.Body.String())
	}
}

func TestImagesInlineDataURL(t *testing.T) {
	st, h, _, px, _ := setup(t)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/health":
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/v1/models" || r.URL.Path == "/v1/images/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"toy-image"}]}`))
		case r.URL.Path == "/v1/videos/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/images/generations":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"created":1,"data":[{"url":"data:image/png;base64,QUJD"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(up.Close)
	bid, err := st.UpsertBackend("comfy", up.URL, true, 1, "opencomfy", "sk-test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveModel(store.Model{
		Alias: "toy-image", UpstreamName: "toy-image", Enabled: true, BackendIDs: []int64{bid},
	}); err != nil {
		t.Fatal(err)
	}
	h.CheckOnce()
	time.Sleep(10 * time.Millisecond)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{"model":"toy-image","prompt":"x"}`))
	rec := httptest.NewRecorder()
	px.ServeImages(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"b64_json":"QUJD"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"url"`) {
		t.Fatalf("url left: %s", rec.Body.String())
	}
}
