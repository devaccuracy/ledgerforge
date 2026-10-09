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

# API security and key operations

Enable `server.secure: true` and keep the API behind TLS. Requests authenticate
with `X-LedgerForge-Key`; the master secret has deployment-wide administrator
power. Use scoped keys for applications and agents. `/health` is public;
`/metrics` uses a separate metrics bearer token. Keep infrastructure and worker
monitoring endpoints private. See [deployment](deployment.md) and the
[private reporting policy](../SECURITY.md).

## Scoped keys and delegation

`POST /api-keys` returns the plaintext key only at creation. Authentication checks
its bcrypt hash, scope, expiry and revocation against PostgreSQL. Key listings
omit stored key material. Preserve the creation response in a secret manager;
you cannot retrieve that secret later.

A non-master caller needs `api-keys:write` to delegate and can create keys only
for its authenticated owner. It may grant only concrete scopes it already
possesses, never broaden to wildcard scopes, and may not grant an expiry later
than its own. Listing and revocation derive the owner from the authenticated
principal; a caller-supplied `owner` query cannot select another owner's keys.
The master key may provision independent owners and scopes.

Delegation ancestry is durable. Descendants inherit restrictions and may narrow
policies; ancestor daily budgets are shared across the delegation tree. An
approver delegated by the initiating key, including an indirect descendant, is
not independent and cannot decide that initiator's hold. Give approver and policy
manager credentials to a separately controlled service or operator. See
[agent safety](agent-safety.md) for policy units and admission semantics.

Resource scopes govern HTTP/MCP operations, but scopes alone do not restrict
transaction amounts or identify a human reviewer. Use spending policies and
independent approval for those controls. Do not give an agent master authority
or the policy-management authority that can change its restrictions.

## Revocation and rotation

`DELETE /api-keys/:id` revokes a key. Authentication reads authoritative
PostgreSQL state instead of accepting cached pre-revocation state; a stale Redis
cache entry cannot restore permission. A completed revocation prevents future
authentication, but does not cancel operations already authenticated, settled
postings or durable admissions accepted beforehand. Review pending activity
when rotating or responding to compromise.

Provision a new scoped key, attach the required policy, deploy it through your
secret manager, verify its access, then revoke the old key. Keep the overlap
short and account for budget use across both identities. Delegated keys share
ancestor budgets; unrelated independently provisioned keys do not automatically
share a limit. Rotate independent approver and signing credentials separately.

## Legacy keys

Older bcrypt rows without a lookup prefix have a compatibility path. Successful
full-secret authentication can backfill the prefix so later requests use indexed
lookup. A hash alone cannot reveal the original prefix, so operators cannot
safely invent one from a stored hash. Rotate active legacy keys instead of
keeping an unbounded legacy population. Expired and revoked credentials must
not authenticate. Indexed lookup runs first; only active prefix-less candidates
enter the fallback. At most two legacy scans run concurrently per process, and
excess HTTP requests receive 429 instead of queuing unlimited bcrypt work.
Startup logs count rows still lacking a prefix. Compatibility is enabled by
default; after migrating clients, set `server.allow_legacy_api_keys: false`
(`LEDGERFORGE_SERVER_ALLOW_LEGACY_API_KEYS`) on every API/MCP process. Indexed
keys continue to authenticate. The concurrency cap does not make a large legacy
population cheap to verify; use rotation and ingress rate limits.

## Input and error handling

Authentication preserves JSON numeric tokens when adding trusted metadata;
monetary conversion uses exact decimal/integer units. Request bodies and uploads
have configurable size bounds. Invalid amount/precision and overdraft controls
are rejected before execution. Treat a parsed JavaScript floating-point value as
a presentation value, not an exact large ledger integer.

API responses preserve their compatible error envelope and stable codes while
sanitizing unstructured backend details. Internal failures return a generic
message; deliberate validation messages remain client-visible. SQL text,
connection strings, raw driver errors and backend detail values belong in
operator logs, not client responses. Policy endpoints have their own public
error shape; do not assume every endpoint wraps a failure identically.

Webhooks use a dedicated signing secret with a legacy master-secret fallback,
protected signature headers and configurable outbound destination controls.
Use constant-time HMAC verification, timestamp freshness and durable receiver
idempotency. See [deployment](deployment.md) and the
[verification example](../examples/webhook-verification/README.md).
