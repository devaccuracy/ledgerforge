<!--
Copyright 2026 LedgerForge Contributors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
-->

# Deployment

The bundled compose files are local development examples. They bind published
ports to `127.0.0.1` and use development database credentials. Production
operators must provide their own secrets, network controls, backup policy and
resource sizing.

## Start from an authenticated configuration

Copy `ledgerforge.example.json` to `ledgerforge.json`, replace the secret-key
placeholder with a random secret and adjust database and Redis addresses.
Compose requires the config file to exist; it mounts it read-only. The example
sets `server.secure: true`, which enables API authentication. Do not disable it
on a shared or reachable deployment.

The master secret authorizes all API operations through `X-LedgerForge-Key`.
Keep it in a secret manager and use scoped API keys for applications. Rotate
keys when access changes. Do not include credentials in images, repositories,
logs, example commands or support reports. Protect the config file on disk.

Legacy bcrypt API keys without a stored prefix remain usable by default through
`server.allow_legacy_api_keys` (`LEDGERFORGE_SERVER_ALLOW_LEGACY_API_KEYS`). Only
active keys enter this compatibility lookup, after indexed lookup finds no
candidate. At most two legacy scans run concurrently per process; excess
requests receive 429 and should retry with backoff. A successful lookup backfills
the prefix. Startup logs report the count of keys still lacking a prefix.
Rotate unused legacy credentials: create replacement keys with the required
owner, scopes, expiry and policy, update clients, then revoke the old keys. After
active clients have migrated, set `server.allow_legacy_api_keys: false` on every
API/MCP process to disable legacy verification. Indexed keys keep working.

Environment variables override loaded JSON fields. Supported settings include
`LEDGERFORGE_SERVER_SECURE`, `LEDGERFORGE_SERVER_SECRET_KEY`,
`LEDGERFORGE_DATA_SOURCE_DNS` and `LEDGERFORGE_REDIS_DNS`. The compose files mount
JSON config; to use environment overrides, explicitly pass them into both the
server and worker containers through a private compose override or your
orchestrator's secret injection.

Do not expose PostgreSQL, Redis, Typesense, tracing UIs or worker monitoring to
the public internet. Isolate those services from application clients. Configure
PostgreSQL TLS and restricted database roles; use Redis TLS with certificate
verification when crossing a trust boundary. Keep credentials consistent in
migrations, API servers and workers.

## TLS and request limits

Terminate TLS at a trusted reverse proxy with request deadlines, body-size
limits and connection limits, then route to the private API listener. Preserve
only the forwarded headers your proxy controls. The local API uses HTTP on
loopback; loopback publication is not a production TLS setup.

Built-in TLS is available through `server.ssl`, `server.domain`,
`server.ssl_email` and `server.cert_storage_path`. ACME issuance requires publicly
reachable challenge ports `80` and `443`, separate from `server.port`. Persist
the certificate directory. The local compose mappings deliberately do not
provide publicly reachable challenge listeners.

Set rate limits for your workload. The code's fallback of 5,000,000 requests per
second is not a useful production limit. For example, start with explicit values
and tune after measuring normal traffic and retry bursts:

```json
{
  "rate_limit": {
    "requests_per_second": 100,
    "burst": 200,
    "cleanup_interval_sec": 60
  }
}
```

The matching environment variables are `LEDGERFORGE_RATE_LIMIT_RPS`,
`LEDGERFORGE_RATE_LIMIT_BURST` and `LEDGERFORGE_RATE_LIMIT_CLEANUP_INTERVAL_SEC`.
These limits are local to a server process. Apply aggregate limits at your
proxy when running multiple replicas. Configure `server.max_request_body_size_mb`
and `server.max_upload_size_mb`; the current defaults are 5 MB and 256 MB.

## Persistence, workers and releases

Run `ledgerforge migrate up` before serving a new version and keep API servers
and workers on compatible versions. Use the tested `:main` image for evaluation;
for production pin a reviewed version or immutable image digest. Tag releases
include both CLIs for Linux, macOS and Windows on amd64 and arm64, SHA-256
checksums, and source/container SPDX SBOMs. Release binaries are built without
CGO; the optional SQLite heartbeat falls back when SQLite is unavailable.

Back up PostgreSQL with a tested restore procedure and retain Redis queue data
according to your recovery requirements. Use durable storage and private
service endpoints. Monitor worker lag, queue saturation, lock contention,
rejected transactions, database health and webhook failures. Search is a
projection; PostgreSQL remains the source of financial truth.

Set `server.metrics_bearer_token` (`LEDGERFORGE_METRICS_BEARER_TOKEN`) before
exposing metrics to a shared monitoring network. Keep worker queue monitoring
private. See [metrics](metrics.md) for endpoints and configuration.

## Webhook verification and destinations

Set a dedicated `notification.webhook.signing_secret`
(`LEDGERFORGE_WEBHOOK_SIGNING_SECRET`) for notification webhooks and transaction
hooks. It takes precedence over `server.secret_key`; the API master secret is a
legacy fallback only when the dedicated value is empty. With both empty,
delivery is unsigned. A dedicated secret lets a receiver verify notifications
without holding API administrator credentials. Store and rotate it separately.

```json
{
  "notification": {
    "webhook": {
      "url": "https://receiver.example/events",
      "signing_secret": "REPLACE_WITH_A_SEPARATE_RANDOM_SECRET",
      "block_private_destinations": true,
      "timeout_sec": 30
    }
  }
}
```

Requests include `X-LedgerForge-Timestamp` (Unix seconds) and
`X-LedgerForge-Signature` (hexadecimal HMAC-SHA256). Verify the exact received
bytes, before parsing JSON:

```text
hex(HMAC-SHA256(secret, timestamp+"."+raw_body))
```

Compare signatures in constant time. Reject malformed or missing headers and
stale timestamps; a five-minute freshness window is a reasonable starting point
with synchronized clocks. Also reject timestamps too far in the future.
Re-serializing JSON changes the signed bytes. Bound the body size before reading
it. The [receiver verification example](../examples/webhook-verification/README.md)
includes freshness, raw-body and wrong-secret regression tests.

Signature and timestamp headers are protected: delivery replaces configured
values with its generated headers, or removes them when no signing secret is
available. Custom headers cannot supply their own signature or timestamp.

Enable `notification.webhook.block_private_destinations`
(`LEDGERFORGE_WEBHOOK_BLOCK_PRIVATE_DESTINATIONS`) for deployments that send only
to public receivers. It defaults to false for compatibility with local/internal
receivers. Hook registration and updates validate absolute HTTP(S) URLs without
credentials or fragments. Destination checks also run when establishing the
connection, with DNS resolution and dialing tied to the permitted addresses.
Redirects are not followed. Restrict egress at the network boundary as well,
especially if you deliberately permit internal receivers.

Notification delivery uses `notification.webhook.timeout_sec`
(`LEDGERFORGE_WEBHOOK_TIMEOUT_SEC`), defaulting to 30 seconds and capped at 120.
Hook response bodies are bounded to 1 MiB. Use HTTPS destinations and restrict
who can register or edit hooks.

Delivery is asynchronous and may be duplicated. Non-2xx responses return an
error for worker retry. Persist the receiver's result before acknowledging with
2xx; use a stable entity/event key and atomic receiver idempotency to avoid
applying a duplicate business operation. Timestamp freshness does not by itself
prevent duplicate processing. See [metadata webhooks](metadata-webhooks.md) for
payloads and [security](security.md) for key management.

## Verification and upgrade operations

Run both accounting replay and audit verification as described in
[verification](verification.md). Enable the hash-chain processor explicitly in
the API server config and retain checkpoint exports outside the ledger database. Plan schema upgrades
with a tested backup, a representative seeded upgrade and a maintenance window
for migrations that backfill or validate transaction history. Measure lock time
on production-sized data; a fresh-database test is insufficient evidence for a
large historical upgrade.

Follow [integrity upgrade maintenance](upgrade-notes.md) for the concrete
execution-control and audit migrations. Stop old servers and workers before
the transition, migrate with a bounded lock timeout, then start compatible
processes and run both read-only verification commands. Concurrent index builds
and separate constraint validation still need I/O capacity and recovery checks.
