package domain

import "time"

type Backend struct {
	ID      int64
	Name    string
	BaseURL string
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

type RequestLog struct {
	ID        int64
	TS        time.Time
	KeyPrefix string
	Model     string
	Backend   string
	Status    int
	LatencyMS int64
	BytesOut  int64
}

type HostStatus struct {
	Healthy bool
	Latency time.Duration
	Error   string
	Checked time.Time
	Models  []string
	Sizes   map[string]int64
	Running []string
}

type CatalogEntry struct {
	Name         string
	BackendIDs   []int64
	BackendNames []string
	BackendCSV   string
	Size         int64
	LoadedOn     []string
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
