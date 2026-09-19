package domain

import "time"

type Backend struct {
	ID      int64
	Name    string
	BaseURL string
	Kind    string
	Token   string
	Enabled bool
	Weight  int
}

type Model struct {
	ID           int64
	Alias        string
	UpstreamName string
	LBPolicy     string
	Enabled      bool
	BackendIDs   []int64
	MaxContext   int
	Fallback     string
	PromptCache  string
	Media        []string
}

type APIKey struct {
	ID            int64
	Name          string
	Prefix        string
	KeyHash       string
	AllowedModels []string
	RPM           int
	Enabled       bool
	CreatedAt     time.Time
	LastUsedAt    *time.Time
	RequestCount  int64
}

func (k APIKey) Allows(model string) bool {
	if len(k.AllowedModels) == 0 {
		return true
	}
	for _, m := range k.AllowedModels {
		if m == "*" || m == model {
			return true
		}
	}
	return false
}

type TokenUsage struct {
	PromptTokens     int
	CompletionTokens int
	CachedTokens     int
	CacheWriteTokens int
	Upstream         string
	PromptUSD        float64
	CacheDiscount    float64
	HasDiscount      bool
	Cost             float64
	HasCost          bool
	SavedUSD         float64
	HasSaved         bool
}

type RequestLog struct {
	ID               int64
	TS               time.Time
	KeyPrefix        string
	Model            string
	Backend          string
	Status           int
	LatencyMS        int64
	BytesOut         int64
	PromptTokens     int
	CompletionTokens int
	CachedTokens     int
	CacheWriteTokens int
	Upstream         string
	PromptUSD        float64
	CacheDiscount    float64
	UsageCost        float64
	SavedUSD         float64
}

type BillingHour struct {
	Hour             time.Time
	N                int
	PromptTokens     int
	CompletionTokens int
	CachedTokens     int
	UsageCost        float64
	SavedUSD         float64
}

type BillingBucket struct {
	Key              string
	Label            string
	Start            time.Time
	N                int
	PromptTokens     int
	CompletionTokens int
	CachedTokens     int
	UsageCost        float64
	SavedUSD         float64
	Pct              int
}

type BillingView struct {
	Period           string
	From             time.Time
	To               time.Time
	N                int
	PromptTokens     int
	CompletionTokens int
	CachedTokens     int
	UsageCost        float64
	SavedUSD         float64
	Buckets          []BillingBucket
}

type HostStatus struct {
	Healthy    bool
	Latency    time.Duration
	Error      string
	Checked    time.Time
	Models     []string
	Sizes      map[string]int64
	Running    []string
	Contexts   map[string]int
	Providers  map[string]string
	Titles     map[string]string
	Prompt     map[string]float64
	Completion map[string]float64
	Priced     map[string]bool
	Media      map[string][]string
	ImageUSD   map[string]float64
	ImageTok   map[string]float64
	VideoSec   map[string]float64
}

type CatalogEntry struct {
	Name          string
	Title         string
	Provider      string
	BackendIDs    []int64
	BackendNames  []string
	BackendCSV    string
	Size          int64
	LoadedOn      []string
	Context       int
	Priced        bool
	PromptUSD     float64
	CompletionUSD float64
	ImageUSD      float64
	ImageTokUSD   float64
	VideoSecUSD   float64
	Media         []string
}

type Job struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	BackendID int64  `json:"backend_id"`
	Backend   string `json:"backend"`
	Model     string `json:"model"`
	Status    string `json:"status"`
	Percent   int    `json:"percent"`
	Message   string `json:"message"`
	Error     string `json:"error,omitempty"`
	Log       string `json:"log"`
}
