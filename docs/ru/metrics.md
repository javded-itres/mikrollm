# Метрики (Prometheus)

Шлюз отдаёт метрики Prometheus по `GET /metrics`. Это **живые** метрики:
`metrics.Collector` обновляет гаджеты каждые 10 с, поэтому Prometheus снимает
свежее состояние на каждом запросе.

## Авторизация

`/metrics` требует **MCP-кен** — тот же, что и у эндпоинта MCP:

```
Authorization: Bearer <MIKROLLM_MCP_TOKEN>
```

Без него: `401 Unauthorized` с `WWW-Authenticate: Bearer realm="MikroLLM metrics"`.

## Включение кена

Задайте `MIKROLLM_MCP_TOKEN` при старте или покрутите в админке
(`/admin` → MCP). Новое значение показывается один раз.

## Метрики

| Метрика | Тип | Метки | Что показывает |
|---------|------|--------|----------------|
| `mikrollm_up` | gauge | — | `1`, когда `GET /metrics` работает. |
| `mikrollm_connections_total` | counter | `key` | Запросы к шлюзу по префиксу API-кен. |
| `mikrollm_requests_total` | counter | `model`, `backend`, `path`, `status` | Запросы, проксированные в бэкенд. |
| `mikrollm_request_duration_seconds` | histogram | `model`, `backend`, `path` | Латентность запросов к бэкенду. |
| `mikrollm_model_up` | gauge | `model`, `enabled` | `1` — модель включена, `0` — выключена. |
| `mikrollm_backend_up` | gauge | `backend`, `kind`, `enabled` | `1` — бэкенд включён и здоров, `0` — выключен/недоступен. |
| `mikrollm_loaded_models` | gauge | — | Количество моделей. |
| `mikrollm_queue_waiting_jobs` | gauge | `queue` | Ожидающих задач в очереди. |
| `mikrollm_queue_waiting_bytes` | gauge | `queue` | Байт в ожидающих задачах очереди. |
| `mikrollm_queue_step_usage` | gauge | `queue`, `step`, `model` | Загрузка степа очереди (0–1). |

## Пример скрапа

```bash
curl -H "Authorization: Bearer $(cat .mcp-token)" \
  http://127.0.0.1:4000/metrics
```

## Конфигурация Prometheus

```yaml
scrape_configs:
  - job_name: mikrollm
    metrics_path: /metrics
    scheme: http
    static_configs:
      - targets: ['127.0.0.1:4000']
    bearer_token: '<MIKROLLM_MCP_TOKEN>'
```
