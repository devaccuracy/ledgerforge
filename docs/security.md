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

Delegation ancestry is immutable and recorded when a scoped key creates another
key. Descendants inherit intersected policies and may narrow them; ancestor
daily budgets are shared across the delegation tree. Delegated members of the
initiator's root cannot decide its hold, including siblings, descendants and
intermediate ancestors. The non-delegated root may decide a descendant's hold
with explicit `approvals:write`, but cannot decide its own. Another root can
provide an approver, including a delegated approver in that independent family;
the master key retains administrator authority. Wildcard scopes alone do not
grant approval or policy-management authority. Give approver and policy-manager
credentials to a separately controlled service or operator. See
[agent safety](agent-safety.md) for policy units and admission semantics.

Keys created before ancestry persistence are treated as roots; historical
delegation cannot be inferred safely from caller metadata. Rotate old delegated
credentials through the authenticated creation path to establish ancestry
before relying on inherited policies or approval-family checks.

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
when rotating or responding to compromise. Revocation applies to that key;
revoke descendant credentials separately when retiring a delegation family.

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
operator logs, not client responses. Inflight commit/void rejects a non-positive
`precise_amount` with `TXN_INVALID_AMOUNT` and `precise_amount must be positive`.
Only exact allowlisted validation messages are preserved from unstructured
errors; messages containing extra backend text use a generic public message.
Policy endpoints have their own public error shape; do not assume every endpoint wraps a failure identically.

Public transaction metadata updates cannot change reserved execution, lineage,
policy or recovery keys. Execution flags are persisted separately as immutable
controls, so customer metadata cannot turn a queued bounded transfer into an
unbounded overdraft or grant policy approval. Customer metadata remains mutable.

Webhooks use a dedicated signing secret with a legacy master-secret fallback,
protected signature headers and configurable outbound destination controls.
Use constant-time HMAC verification, timestamp freshness and durable receiver
idempotency. See [deployment](deployment.md) and the
[verification example](../examples/webhook-verification/README.md).
