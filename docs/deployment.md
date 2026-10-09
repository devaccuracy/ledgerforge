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

## Webhook verification

The current notification webhook signs the raw JSON body using
`server.secret_key`. Requests include `X-LedgerForge-Timestamp` (Unix seconds)
and `X-LedgerForge-Signature` (a hexadecimal HMAC-SHA256 digest). Verify:

```text
expected = hex(HMAC-SHA256(secret, timestamp + "." + raw_request_body))
```

Use a constant-time comparison. Reject missing or malformed headers and
stale timestamps; choose a freshness window such as five minutes with
synchronized clocks. Read and bound the raw body before decoding JSON; signing
re-serialized JSON will not reliably reproduce the signature. Since the current
signing secret is also the API master secret, a receiver holding it has ledger
administrator credentials: keep receivers in the same trust boundary.

Do not configure notification headers with either reserved signature header;
the current configuration can override them. With an empty signing secret,
notification delivery is unsigned. Keep an explicit nonempty secret and verify
signatures at the receiver.

Delivery is asynchronous and can be duplicated. Non-2xx responses return an
error for worker retry. Persist the receiver's result before acknowledging with
2xx and deduplicate by a stable entity/event key suitable for your workflow.
A timestamp freshness check alone does not prevent duplicate processing.

Use HTTPS destinations, restrict hook registration permissions and control
outbound network access. Review registered hook URLs and redirect behavior;
authentication alone does not make outbound destinations safe. See
[metadata webhooks](metadata-webhooks.md) for event payloads.

<!-- Integration placeholder: update signing-secret and outbound-destination guidance after the security changes merge. -->
