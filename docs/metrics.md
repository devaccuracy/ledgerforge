# LedgerForge Metrics Reference

LedgerForge exposes OpenTelemetry metrics via a Prometheus-compatible `/metrics` endpoint. This document lists all available metrics, their types, attributes, and what they tell you operationally.

## Prerequisites

- Set `"enable_observability": true` in `ledgerforge.json` (or `LEDGERFORGE_ENABLE_OBSERVABILITY=true`)
- Metrics are served on:
  - **Server**: `GET /metrics` on the API port (default `5001`)
  - **Worker**: `GET /metrics` on the monitoring port (default `5004`)

## Authentication

Set a metrics bearer token to require it even when `server.secure` is disabled.
With secure mode enabled, a token is required:

1. Set `"server": {"metrics_bearer_token": "<your-token>"}` in `ledgerforge.json` (or `LEDGERFORGE_METRICS_BEARER_TOKEN`)
2. Configure Prometheus to send the token:

```yaml
scrape_configs:
  - job_name: 'ledgerforge-server'
    authorization:
      type: Bearer
      credentials: '<your-token>'
    static_configs:
      - targets: ['server:5001']
```

If secure mode is enabled without a token configured, the endpoint returns `403 Forbidden`.
The worker `/monitoring/` dashboard uses the same bearer-token rules; `/health`
remains public. Keep the monitoring listener on a private network.

## Export Modes

| Mode | How it works | When active |
|------|-------------|-------------|
| **Pull (Prometheus)** | Prometheus scrapes `/metrics` | Always (when observability is enabled) |
| **Push (OTLP HTTP)** | Periodically pushes to an OTel Collector | When `OTEL_EXPORTER_OTLP_METRICS_ENDPOINT` or `OTEL_EXPORTER_OTLP_ENDPOINT` is set |

## Metrics

> **Naming convention**: OTel instrument names use dots (e.g., `ledgerforge.transaction.total`). The Prometheus exporter converts these to underscores automatically (e.g., `ledgerforge_transaction_total`).

### Transaction Metrics

| Prometheus Name | Type | Attributes | Description |
|----------------|------|------------|-------------|
| `ledgerforge_transaction_total` | Counter | `status`, `currency` | Total transactions by final status and currency. Use to track throughput and status distribution. |
| `ledgerforge_transaction_duration_seconds` | Histogram | `status` | Wall-clock time of `RecordTransaction()` in seconds. Use to monitor processing latency and set SLOs. |
| `ledgerforge_transaction_rejected_total` | Counter | `reason` | Rejected transactions by reason. Use to alert on elevated rejection rates. |

**`status` values** reflect the recorded transaction state, such as `APPLIED`,
`REJECTED`, `INFLIGHT` or `VOID`. Duration also records `status="error"` when no
transaction result is available.

**`reason` values**: `insufficient_funds`, `overdraft_limit`, `lock_contention`, `max_retries`, `other`

**`currency` values**: Determined by the transactions processed (e.g., `USD`, `NGN`, `EUR`)

### Queue Metrics

| Prometheus Name | Type | Attributes | Description |
|----------------|------|------------|-------------|
| `ledgerforge_queue_enqueued_total` | Counter | `queue_name` | Transactions enqueued for async processing. Use to monitor inflow rate per queue. |
| `ledgerforge_queue_backpressure_rejected_total` | Counter | `reason` | Enqueue attempts rejected by the queue capacity limit. |
| `ledgerforge_queue_processing_duration_seconds` | Histogram | `result` | Time spent processing a transaction in the worker. Use to detect worker slowdowns. |

**`queue_name` values**: `new:transaction_1` through `new:transaction_N` (sharded queues), `hot_transactions` (hot lane)

**`result` values**: `success`

### Search Backpressure Metrics

| Prometheus Name | Type | Attributes | Description |
| --- | --- | --- | --- |
| `ledgerforge_search_backpressure_retry_total` | Counter | `source` | Indexing retries caused by Typesense memory backpressure; reindex uses `source="reindex"`. |

### Balance Metrics

| Prometheus Name | Type | Attributes | Description |
|----------------|------|------------|-------------|
| `ledgerforge_balance_created_total` | Counter | — | Total balances created. Use to track account growth. |

### Inflight Transaction Metrics

| Prometheus Name | Type | Attributes | Description |
|----------------|------|------------|-------------|
| `ledgerforge_inflight_commit_total` | Counter | — | Inflight transactions committed. |
| `ledgerforge_inflight_void_total` | Counter | — | Inflight transactions voided. Use the commit/void ratio to monitor authorization patterns. |

### Batch Coalescing Metrics

| Prometheus Name | Type | Attributes | Description |
|----------------|------|------------|-------------|
| `ledgerforge_transaction_batch_size` | Histogram | — | Number of transactions per coalesced batch. Use to evaluate batching efficiency. |
| `ledgerforge_transaction_batch_total` | Counter | `result` | Coalescing attempts by outcome. |

**`result` values**: `success`, `failure`, `skipped`

### Hot Pairs / Lock Contention Metrics

| Prometheus Name | Type | Attributes | Description |
|----------------|------|------------|-------------|
| `ledgerforge_hotpairs_contention_total` | Counter | — | Lock acquisition failures. Spikes indicate contention on specific balance pairs. |
| `ledgerforge_hotpairs_lane_routed_total` | Counter | `lane` | Transactions routed to queue lanes. Use to verify hot lane is activating as expected. |

**`lane` values**: `normal`, `hot`

### Worker Metrics

| Prometheus Name | Type | Attributes | Description |
|----------------|------|------------|-------------|
| `ledgerforge_worker_retries_total` | Counter | `reason` | Worker retry events. Sustained retries indicate systemic issues. |

**`reason` values**: `insufficient_funds`, `other`

### Audit Chain Metrics

| Prometheus Name | Type | Attributes | Description |
| --- | --- | --- | --- |
| `ledgerforge_chain_backlog` | Gauge | — | Unsealed rows older than the configured trailing delay. |
| `ledgerforge_chain_head_seq` | Gauge | — | Last sealed sequence. |
| `ledgerforge_chain_lag_seconds` | Gauge | — | Seconds since chain state was last updated. |

The API server's enabled hash-chain processor records these gauges on its poll
ticks. Backlog excludes rows inside the trailing delay, while `audit verify`
reports all unsealed rows as pending. An idle ledger can have increasing lag
with zero backlog; interpret lag with recent traffic and coverage. See
[verification](verification.md) for pending coverage and retained roots.

## Example Prometheus Queries

```promql
# Transaction throughput (per second, 5 minute window)
rate(ledgerforge_transaction_total[5m])

# Rejection rate by reason
rate(ledgerforge_transaction_rejected_total[5m])

# P99 transaction latency
histogram_quantile(0.99, rate(ledgerforge_transaction_duration_seconds_bucket[5m]))

# Queue enqueue rate
rate(ledgerforge_queue_enqueued_total[5m])

# Hot lane traffic ratio
ledgerforge_hotpairs_lane_routed_total{lane="hot"} / ledgerforge_hotpairs_lane_routed_total

# Worker retry rate
rate(ledgerforge_worker_retries_total[5m])

# Batch coalescing success rate
ledgerforge_transaction_batch_total{result="success"} / ledgerforge_transaction_batch_total
```
