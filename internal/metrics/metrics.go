// Package metrics собирает Prometheus-метрики состояния MikroLLM: загрузка,
// соединения, состояние бэкендов/моделей/очередей, загруженные модели.
//
// Метрики живые (gauges), поэтому Prometheus ходит на /metrics по расписанию.
// Приём запросов учитывается счётчиками, которые инкрементируются в обёртке
// metrics.Store вокруг ports.Store.
package metrics

import (
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/javded-itres/mikrollm/internal/domain"
	"github.com/javded-itres/mikrollm/internal/ports"
	"github.com/javded-itres/mikrollm/internal/queue"
	"github.com/javded-itres/mikrollm/internal/store"
)

// Namespace для всех метрик.
const Namespace = "mikrollm"

var (
	registerOnce sync.Once
	up           = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "up",
		Help:      "1 — сервис запущен и отвечает на /metrics.",
	})

	connections = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Name:      "connections_total",
		Help:      "Количество запросов к шлюзу от ключа (key).",
	}, []string{"key"})

	requests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Name:      "requests_total",
		Help:      "Количество запросов к бэкенду (model, backend, path, status).",
	}, []string{"model", "backend", "path", "status"})

	requestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Name:      "request_duration_seconds",
		Help:      "Задержка обработки запросов к бэкенду (модель, бэкенд, путь).",
		Buckets:   prometheus.DefBuckets,
	}, []string{"model", "backend", "path"})

	// Состояние моделей: 1 — модель включена, 0 — выключена.
	modelUp = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "model_up",
		Help:      "1 — модель включена и доступна, 0 — выключена (model, enabled).",
	}, []string{"model", "enabled"})

	// Состояние бэкендов: 1 — включён и отвечает, 0 — выключен/недоступен.
	backendUp = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "backend_up",
		Help:      "1 — бэкенд включён и отвечает, 0 — выключен или недоступен (backend, kind, enabled).",
	}, []string{"backend", "kind", "enabled"})

	queueWaitingJobs = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "queue_waiting_jobs",
		Help:      "Количество ждущих jobs в очереди (queue).",
	}, []string{"queue"})

	queueWaitingBytes = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "queue_waiting_bytes",
		Help:      "Байты в ждущих jobs очередей (queue).",
	}, []string{"queue"})

	queueStepUsage = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "queue_step_usage",
		Help:      "Загрузка шага очереди в долях единицы (queue, step, model).",
	}, []string{"queue", "step", "model"})

	loadedModels = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "loaded_models",
		Help:      "Количество загруженных моделей в данный момент.",
	})
)

// Handler возвращает http-обработчик для /metrics.
func Handler() http.Handler {
	return promhttp.HandlerFor(prometheus.DefaultGatherer, promhttp.HandlerOpts{
		ErrorHandling: promhttp.ContinueOnError,
	})
}

// RegisterCollector регистрирует все метрики в DefaultRegistry и возвращает себя.
// Устойчив к повторным вызовам: метрики регистрируются один раз.
func RegisterCollector() *Collector {
	c := &Collector{}
	registerOnce.Do(func() {
		prometheus.MustRegister(up)
		prometheus.MustRegister(connections)
		prometheus.MustRegister(requests)
		prometheus.MustRegister(requestDuration)
		prometheus.MustRegister(modelUp)
		prometheus.MustRegister(backendUp)
		prometheus.MustRegister(queueWaitingJobs)
		prometheus.MustRegister(queueWaitingBytes)
		prometheus.MustRegister(queueStepUsage)
		prometheus.MustRegister(loadedModels)
	})
	return c
}

// Collector группирует состояние для актуализации по запросу /metrics.
type Collector struct {
	mu    sync.Mutex
	store *store.Store
	queue *queue.Engine
}

// SetDeps передает зависимости для живого сбора состояния.
func (c *Collector) SetDeps(st *store.Store, q *queue.Engine) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store = st
	c.queue = q
}
func (c *Collector) Snapshot() {
	up.Set(1)

	c.mu.Lock()
	st, q := c.store, c.queue
	c.mu.Unlock()

	if st != nil {
		if err := collectBackends(st); err != nil {
			log.Printf("metrics backends: %v", err)
		}
		if err := collectModels(st); err != nil {
			log.Printf("metrics models: %v", err)
		}
	}
	if q != nil {
		if err := collectQueue(q); err != nil {
			log.Printf("metrics queue: %v", err)
		}
	}
}

func collectBackends(st *store.Store) error {
	backends, err := st.ListBackends()
	if err != nil {
		return err
	}
	backendUp.Reset()
	for _, b := range backends {
		enabled := "0"
		if b.Enabled {
			enabled = "1"
		}
		backendUp.WithLabelValues(b.Name, b.Kind, enabled).Set(boolOne(b.Enabled))
	}
	return nil
}

func collectModels(st *store.Store) error {
	models, err := st.ListModels()
	if err != nil {
		return err
	}
	modelUp.Reset()
	for _, m := range models {
		enabled := "0"
		if m.Enabled {
			enabled = "1"
		}
		modelUp.WithLabelValues(m.Alias, enabled).Set(boolOne(m.Enabled))
	}
	loadedModels.Set(float64(len(models)))
	return nil
}

func collectQueue(q *queue.Engine) error {
	snap := q.Snapshot()
	queueWaitingJobs.Reset()
	queueWaitingBytes.Reset()
	queueStepUsage.Reset()
	for _, v := range snap {
		queueWaitingJobs.WithLabelValues(v.Name).Set(float64(v.Waiting))
		queueWaitingBytes.WithLabelValues(v.Name).Set(float64(v.Bytes))
		for i, step := range v.Steps {
			usage := 0.0
			if step.Cap > 0 {
				usage = float64(step.Busy) / float64(step.Cap)
			}
			queueStepUsage.WithLabelValues(v.Name, strconv.Itoa(i), step.Alias).Set(usage)
		}
	}
	return nil
}

func boolOne(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// RecordRequest инкрементирует request-метрики после обработки запроса.
func RecordRequest(model, backend, path string, status int, latency time.Duration, bytesOut int64) {
	requests.WithLabelValues(model, backend, path, strconv.Itoa(status)).Inc()
	requestDuration.WithLabelValues(model, backend, path).Observe(latency.Seconds())
}

// RecordConnection инкрементирует счётчик соединений.
func RecordConnection(key, path string) {
	connections.WithLabelValues(key, path).Inc()
}

// Store — обёртка вокруг ports.Store, которая перехватывает Log и считает
// запросы к бэкенду. Передаётся в proxy.New вместо исходного store.
type Store struct {
	ports.Store
	hook func(prefix, model, backend string, status int, latency time.Duration, bytesOut int64)
}

// NewStore оборачивает st, подключая request-метрики к hook.
func NewStore(st ports.Store, hook func(prefix, model, backend string, status int, latency time.Duration, bytesOut int64)) *Store {
	return &Store{Store: st, hook: hook}
}

// Log записывает запрос в хранилище и инкрементирует request-метрики.
func (s *Store) Log(prefix, model, backend string, status int, latency time.Duration, bytesOut int64, u domain.TokenUsage) {
	s.Store.Log(prefix, model, backend, status, latency, bytesOut, u)
	if s.hook != nil {
		s.hook(prefix, model, backend, status, latency, bytesOut)
	}
}
