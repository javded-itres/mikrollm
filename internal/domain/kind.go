package domain

import "strings"

const (
	KindOllama      = "ollama"
	KindVLLM        = "vllm"
	KindLMStudio    = "lmstudio"
	KindOpenRouter  = "openrouter"
	KindOllamaCloud = "ollama-cloud"
	KindOpenComfy   = "opencomfy"
	KindHub         = "hub"

	// VLLMLoadHelp is shown when the user tries to pull/load/unload via API.
	// vLLM binds one model to the process for its lifetime.
	VLLMLoadHelp = "vLLM держит одну модель в памяти процесса. Остановите сервер и запустите: vllm serve <HuggingFace-id> --host 0.0.0.0 --port 8000. Подробнее: docs/providers.md"

	DefaultOpenRouterURL  = "https://openrouter.ai/api/v1"
	DefaultOllamaCloudURL = "https://ollama.com"
)

func NormalizeKind(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case KindVLLM, "v-llm":
		return KindVLLM
	case KindLMStudio, "lm-studio", "lms", "llstudio", "lm studio":
		return KindLMStudio
	case KindOpenRouter, "open-router", "open router":
		return KindOpenRouter
	case KindOllamaCloud, "ollamacloud", "ollama_cloud", "ollama cloud":
		return KindOllamaCloud
	case KindOpenComfy, "open-comfy", "open comfy", "comfyui", "comfy":
		return KindOpenComfy
	case KindHub, "mikrollm-hub", "mikrollm_hub":
		return KindHub
	default:
		return KindOllama
	}
}

func RequiresToken(kind string) bool {
	k := NormalizeKind(kind)
	return k == KindOpenRouter || k == KindOllamaCloud || k == KindOpenComfy
}

func CanonicalBaseURL(kind, raw string) string {
	u := strings.TrimRight(strings.TrimSpace(raw), "/")
	switch NormalizeKind(kind) {
	case KindOpenRouter:
		if u == "" || u == "https://openrouter.ai" || u == "http://openrouter.ai" {
			return DefaultOpenRouterURL
		}
	case KindOllamaCloud:
		if u == "" || u == "https://www.ollama.com" {
			return DefaultOllamaCloudURL
		}
	case KindOpenComfy:
		u = strings.TrimSuffix(u, "/v1")
		u = strings.TrimRight(u, "/")
	}
	return u
}

func (b Backend) KindNorm() string { return NormalizeKind(b.Kind) }

func (b Backend) KindClass() string {
	switch b.KindNorm() {
	case KindVLLM:
		return "kind-vllm"
	case KindLMStudio:
		return "kind-lms"
	case KindOpenRouter:
		return "kind-or"
	case KindOllamaCloud:
		return "kind-oc"
	case KindHub:
		return "kind-hub"
	case KindOpenComfy:
		return "kind-comfy"
	default:
		return "kind-ollama"
	}
}

func (b Backend) Label() string {
	switch b.KindNorm() {
	case KindVLLM:
		return "vLLM"
	case KindLMStudio:
		return "LM Studio"
	case KindOpenRouter:
		return "OpenRouter"
	case KindOllamaCloud:
		return "Ollama Cloud"
	case KindHub:
		if b.Name != "" {
			return "hub · " + b.Name
		}
		return "Hub"
	case KindOpenComfy:
		return "OpenComfy"
	default:
		return "Ollama"
	}
}

func (b Backend) Cloud() bool {
	k := b.KindNorm()
	return k == KindOpenRouter || k == KindOllamaCloud
}

func (b Backend) NativeOllama() bool {
	k := b.KindNorm()
	return k == KindOllama || k == KindOllamaCloud
}

func (b Backend) CanLoad() bool {
	k := b.KindNorm()
	return k == KindOllama || k == KindLMStudio
}

func (b Backend) CanPull() bool {
	k := b.KindNorm()
	return k == KindOllama || k == KindLMStudio
}

func (b Backend) CanDelete() bool {
	return b.KindNorm() == KindOllama
}

func (b Backend) ChatPath() string {
	if b.NativeOllama() {
		return "/api/chat"
	}
	return b.OpenAIChatPath()
}

func (b Backend) OpenAIChatPath() string {
	if b.KindNorm() == KindOpenRouter {
		return "/chat/completions"
	}
	return "/v1/chat/completions"
}

func (b Backend) ModelsPath() string {
	switch b.KindNorm() {
	case KindOpenRouter:
		return "/models"
	case KindOllama, KindOllamaCloud:
		return "/api/tags"
	case KindLMStudio:
		return "/api/v1/models"
	default:
		return "/v1/models"
	}
}

func (b Backend) LoadHint() string {
	switch b.KindNorm() {
	case KindVLLM:
		return VLLMLoadHelp
	case KindOpenRouter:
		return "OpenRouter — облако: модели уже у провайдера. Скачивание и RAM на вашей машине не нужны. Подробнее: docs/providers.md#openrouter"
	case KindOllamaCloud:
		return "Ollama Cloud — только облачные модели на ollama.com, без локального Ollama. Подробнее: docs/providers.md#ollama-cloud"
	case KindOpenComfy:
		return "OpenComfy — шлюз к ComfyUI (картинки и видео). Веса и workflow на GPU-сервере, не через MikroLLM. Подробнее: docs/providers.md#opencomfy"
	default:
		return ""
	}
}

func (b Backend) UIHint() string {
	switch b.KindNorm() {
	case KindVLLM:
		return "Модель задаётся при старте: vllm serve <HuggingFace-id> --host 0.0.0.0 --port 8000"
	case KindOpenRouter:
		return "Прямое облако. Ключ: openrouter.ai/keys. 403 — контейнер должен ходить в интернет через VPN, не ISP РФ."
	case KindOllamaCloud:
		return "Облачные модели ollama.com, локальный Ollama не нужен. Ключ: ollama.com/settings/keys"
	case KindOpenComfy:
		return "ComfyUI через OpenComfy. URL без /v1 (например http://192.168.88.252:8788). Ключ sk- из keys.yaml OpenComfy."
	default:
		return ""
	}
}

func (b Backend) DocsURL() string {
	base := "https://github.com/javded-itres/mikrollm/blob/main/docs/providers.md"
	switch b.KindNorm() {
	case KindVLLM:
		return base + "#vllm"
	case KindOpenRouter:
		return base + "#openrouter"
	case KindOllamaCloud:
		return base + "#ollama-cloud"
	case KindLMStudio:
		return base + "#lm-studio"
	case KindOpenComfy:
		return base + "#opencomfy"
	default:
		return base
	}
}

func (b Backend) DefaultPort() string {
	switch b.KindNorm() {
	case KindVLLM:
		return "8000"
	case KindLMStudio:
		return "1234"
	case KindOpenRouter, KindOllamaCloud:
		return "443"
	case KindOpenComfy:
		return "8788"
	default:
		return "11434"
	}
}
