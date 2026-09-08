package domain

import "time"

type QueueStep struct {
	Pos           int    `json:"pos"`
	ModelAlias    string `json:"model_alias"`
	MaxConcurrent int    `json:"max_concurrent"`
}

type Queue struct {
	ID            int64       `json:"id"`
	Name          string      `json:"name"`
	Alias         string      `json:"alias"`
	Enabled       bool        `json:"enabled"`
	OverflowAfter int         `json:"overflow_after"`
	OverflowAlias string      `json:"overflow_alias"`
	MaxWaitMS     int         `json:"max_wait_ms"`
	Steps         []QueueStep `json:"steps"`
	ExtraAliases  []string    `json:"extra_aliases"`
}

func (q Queue) AllAliases() []string {
	out := []string{q.Alias}
	for _, a := range q.ExtraAliases {
		if a != "" && a != q.Alias {
			out = append(out, a)
		}
	}
	return out
}

type QueueJob struct {
	ID              string
	QueueID         int64
	Seq             int64
	Status          string
	Alias           string
	AssignedModel   string
	AssignedBackend string
	Provider        string
	KeyPrefix       string
	Path            string
	Body            []byte
	Bytes           int
	Error           string
	CreatedAt       time.Time
	StartedAt       *time.Time
	FinishedAt      *time.Time
}

const (
	QueueWaiting  = "waiting"
	QueueRunning  = "running"
	QueueDone     = "done"
	QueueError    = "error"
	QueueDropped  = "dropped"
	QueueOverflow = "overflow"
	QueueCanceled = "canceled"
)

type QueueStepView struct {
	Alias    string `json:"alias"`
	Provider string `json:"provider"`
	Busy     int    `json:"busy"`
	Cap      int    `json:"cap"`
	Percent  int    `json:"percent"`
	Healthy  bool   `json:"healthy"`
}

type QueueJobView struct {
	Seq       int64  `json:"seq"`
	Status    string `json:"status"`
	Alias     string `json:"alias"`
	Model     string `json:"model"`
	Backend   string `json:"backend"`
	Provider  string `json:"provider"`
	KeyPrefix string `json:"key_prefix"`
	Error     string `json:"error,omitempty"`
	AgeMS     int64  `json:"age_ms"`
}

type QueueView struct {
	ID            int64           `json:"id"`
	Name          string          `json:"name"`
	Alias         string          `json:"alias"`
	Waiting       int             `json:"waiting"`
	Running       int             `json:"running"`
	Bytes         int64           `json:"bytes"`
	OverflowAfter int             `json:"overflow_after"`
	OverflowAlias string          `json:"overflow_alias"`
	Steps         []QueueStepView `json:"steps"`
	Jobs          []QueueJobView  `json:"jobs"`
	ExtraAliases  []string        `json:"extra_aliases"`
}
