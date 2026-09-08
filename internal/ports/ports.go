package ports

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/javded-itres/mikrollm/internal/domain"
)

var ErrBusy = errors.New("busy")

type HTTPGetter interface {
	Get(url string) (*http.Response, error)
}

type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type BackendQuery interface {
	ListBackends() ([]domain.Backend, error)
	GetBackend(id int64) (domain.Backend, error)
}

type BackendCommand interface {
	UpsertBackend(name, baseURL string, enabled bool, weight int, kind, token string) (int64, error)
	DeleteBackend(id int64) error
}

type ModelRepo interface {
	ListModels() ([]domain.Model, error)
	GetModel(id int64) (domain.Model, error)
	GetModelByAlias(alias string) (domain.Model, error)
	SaveModel(m domain.Model) (int64, error)
	DeleteModel(id int64) error
	ConnectOllamaModel(name string, backendIDs []int64, policy string, maxContext int) error
}

type QueueRepo interface {
	ListQueues() ([]domain.Queue, error)
	GetQueue(id int64) (domain.Queue, error)
	GetQueueByAlias(alias string) (domain.Queue, error)
	SaveQueue(q domain.Queue) (int64, error)
	DeleteQueue(id int64) error
	AddQueueAlias(queueID int64, alias string) error
	DeleteQueueAlias(queueID int64, alias string) error
	InsertQueueJob(j *domain.QueueJob) error
	UpdateQueueJob(j domain.QueueJob) error
	GetQueueJob(id string) (domain.QueueJob, error)
	ListQueueJobs(queueID int64, limit int) ([]domain.QueueJob, error)
	WaitingStats() (jobs int, bytes int64, err error)
	OldestWaiting(n int) ([]domain.QueueJob, error)
	ResetStaleQueueJobs() error
	PurgeFinishedQueueJobs(keep int, maxAge time.Duration) error
	AliasTaken(alias string, exceptQueueID, exceptModelID int64) (bool, error)
}

type KeyRepo interface {
	GetKeyByHash(hash string) (domain.APIKey, error)
	GetKey(id int64) (domain.APIKey, error)
	ListKeys() ([]domain.APIKey, error)
	InsertKey(k domain.APIKey) (int64, error)
	UpdateKey(k domain.APIKey) error
	DeleteKey(id int64) error
	TouchKey(id int64)
}

type SecretRepo interface {
	AdminHash() (string, error)
	SessionSecret() (string, error)
	SetAdminPassword(password string) error
	EnsureAdmin(password string, reset bool) error
	MCPTokenHash() (string, error)
	MCPTokenPrefix() (string, error)
	SetMCPToken(plain string) (prefix string, err error)
	EnsureMCPToken(plain string, reset bool) (generated string, err error)
}

type LogRepo interface {
	Log(prefix, model, backend string, status int, latency time.Duration, bytesOut int64)
	ListLogs(limit int) ([]domain.RequestLog, error)
}

type JobRepo interface {
	UpsertJob(j domain.Job) error
	ListJobs() ([]domain.Job, error)
}

type AuthStore interface {
	GetKeyByHash(hash string) (domain.APIKey, error)
	AdminHash() (string, error)
	SessionSecret() (string, error)
	MCPTokenHash() (string, error)
}

type Store interface {
	BackendQuery
	BackendCommand
	ModelRepo
	QueueRepo
	KeyRepo
	SecretRepo
	LogRepo
	SeedIfEmpty(backends []domain.Backend) error
	Close() error
}

type Health interface {
	CheckOnce()
	Get(id int64) domain.HostStatus
	Catalog(backends []domain.Backend) []domain.CatalogEntry
	HealthyCount() int
	Inc(id int64)
	Dec(id int64)
	Conns(id int64) int
	HasModel(id int64, name string) bool
}

type Auth interface {
	Authenticate(plain string) (domain.APIKey, error)
	AllowRPM(k domain.APIKey) bool
	CheckPassword(pw string) bool
	ValidMCP(token string) bool
	LoginBlocked(ip string) bool
	RecordLogin(ip string, ok bool)
	IssueCookie(w http.ResponseWriter, r *http.Request) error
	ClearCookie(w http.ResponseWriter)
	ValidSession(r *http.Request) bool
	CSRF(r *http.Request) string
	ValidCSRF(r *http.Request, tok string) bool
	PutFlash(w http.ResponseWriter, val string)
	TakeFlash(w http.ResponseWriter, r *http.Request) string
}

type Host interface {
	Pull(ctx context.Context, b domain.Backend, model string, w io.Writer) error
	Delete(ctx context.Context, b domain.Backend, model string) error
	Unload(ctx context.Context, b domain.Backend, model string) error
	Load(ctx context.Context, b domain.Backend, model string) error
}

type ChatGateway interface {
	ServeChat(w http.ResponseWriter, r *http.Request)
}

type Jobs interface {
	Begin(kind string, backendID int64, backend, model string) (created *domain.Job, busy *domain.Job, err error)
	Writer(id string) io.WriteCloser
	Fail(id, msg string)
	Done(id string)
	SetMessage(id, msg string)
	List() []domain.Job
	Running() bool
}
