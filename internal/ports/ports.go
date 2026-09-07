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
	UpsertBackend(name, baseURL string, enabled bool, weight int) (int64, error)
	DeleteBackend(id int64) error
}

type ModelRepo interface {
	ListModels() ([]domain.Model, error)
	GetModelByAlias(alias string) (domain.Model, error)
	SaveModel(m domain.Model) (int64, error)
	DeleteModel(id int64) error
	ConnectOllamaModel(name string, backendIDs []int64, policy string) error
}

type KeyRepo interface {
	GetKeyByHash(hash string) (domain.APIKey, error)
	ListKeys() ([]domain.APIKey, error)
	InsertKey(k domain.APIKey) (int64, error)
	DeleteKey(id int64) error
	TouchKey(id int64)
}

type SecretRepo interface {
	AdminHash() (string, error)
	SessionSecret() (string, error)
	SetAdminPassword(password string) error
	EnsureAdmin(password string, reset bool) error
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
}

type Store interface {
	BackendQuery
	BackendCommand
	ModelRepo
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
	LoginBlocked(ip string) bool
	RecordLogin(ip string, ok bool)
	IssueCookie(w http.ResponseWriter, r *http.Request) error
	ClearCookie(w http.ResponseWriter)
	ValidSession(r *http.Request) bool
}

type Host interface {
	Pull(ctx context.Context, base, model string, w io.Writer) error
	Delete(ctx context.Context, base, model string) error
	Unload(ctx context.Context, base, model string) error
	Load(ctx context.Context, base, model string) error
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
