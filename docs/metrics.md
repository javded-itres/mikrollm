# Metrics (Prometheus)

The gateway exposes Prometheus metrics at `GET /metrics`. They are **live**:
`metrics.Collector` refreshes gauges every 10 s, so Prometheus scrapes fresh
state on each request.

## Auth

`/metrics` requires a **MCP bearer token** — the same token used by the MCP
endpoint:

```
Authorization: Bearer <MIKROLLM_MCP_TOKEN>
```

Without it: `401 Unauthorized` with `WWW-Authenticate: Bearer realm="MikroLLM metrics"`.

## Enable the token

Set `MIKROLLM_MCP_TOKEN` on start, or rotate it in the admin panel
(`/admin` → MCP). The new value is shown once.

## Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `mikrollm_up` | gauge | — | `1` when `GET /metrics` works. |
| `mikrollm_connections_total` | counter | `key` | Requests to the gateway by API key prefix. |
| `mikrollm_requests_total` | counter | `model`, `backend`, `path`, `status` | Requests proxied to a backend. |
| `mikrollm_request_duration_seconds` | histogram | `model`, `backend`, `path` | Backend request latency. |
| `mikrollm_model_up` | gauge | `model`, `enabled` | `1` — model enabled, `0` — disabled. |
| `mikrollm_backend_up` | gauge | `backend`, `kind`, `enabled` | `1` — backend enabled and healthy, `0` — disabled/down. |
| `mikrollm_loaded_models` | gauge | — | Number of models. |
| `mikrollm_queue_waiting_jobs` | gauge | `queue` | Waiting jobs in the queue. |
| `mikrollm_queue_waiting_bytes` | gauge | `queue` | Bytes in waiting queue jobs. |
| `mikrollm_queue_step_usage` | gauge | `queue`, `step`, `model` | Queue step load (0–1). |

## Example scrape

```bash
curl -H "Authorization: Bearer $(cat .mcp-token)" \
  http://127.0.0.1:4000/metrics
```

## Prometheus scrape config

```yaml
scrape_configs:
  - job_name: mikrollm
    metrics_path: /metrics
    scheme: http
    static_configs:
      - targets: ['127.0.0.1:4000']
    bearer_token: '<MIKROLLM_MCP_TOKEN>'
```
