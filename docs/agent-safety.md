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

# Agent spending policies and approval

Bind each agent to its own API key, narrow resource scopes and a server-enforced
spending policy. The same admission path protects HTTP and identity-bound MCP
transactions; policy decisions and usage are persisted in PostgreSQL before
asynchronous processing. Prompts, caller metadata and `confirm: true` do not
supply independent approval authority.

## Policy schema and admission

An administrator or a key with explicit `policies:write` can set a key policy:

```text
PUT /policies/:api_key_id
X-LedgerForge-Key: <administrator or policy manager key>
```

```json
{
  "rules": [
    {
      "currency": "USD",
      "precision": 100,
      "max_transaction": "5000",
      "daily_limit": "10000",
      "approval_above": "2000"
    }
  ],
  "ledger_ids": ["<permitted ledger_id>"],
  "balance_ids": ["<source balance_id>", "<destination balance_id>"]
}
```

Policy amounts are **canonical nonnegative integer strings**, already in the
rule's units; `"5000"` at precision `100` means `50.00`. Currency/precision pairs
are explicit allow rules: a missing pair denies admission. Nonempty ledger and
balance lists must admit **both** endpoints of every resolved leg. An empty list
adds no restriction at that policy level. The policy's limits apply to aggregate
amounts per currency/precision for split and bulk admissions; splitting a request
does not multiply its permitted transaction amount.

`max_transaction` denies an admission above the cap. `daily_limit` constrains
settled spend plus admitted queued, scheduled and held reservations.
`approval_above` requires approval only when the aggregate amount is strictly
greater than the threshold. A missing numeric limit leaves that dimension
uncapped; `"0"` is a real zero limit. Policy versions are persisted with admissions.
Keys without a policy or inherited restrictions retain the legacy behavior.

Do not grant an agent `policies:write`, administrator authority or unrelated
approval authority. Policy-management scopes are explicit administrative powers;
a policy is not a sandbox around a credential allowed to change its own rules.

## Delegation and shared capacity

Delegated keys inherit their ancestors' restrictions. A child may narrow scopes,
expiry, currency/precision permissions, allowlists and spending limits, but cannot
remove a parent's constraints. Parent and sibling delegations share the ancestor's
daily budget: creating ten keys does not create ten copies of its allowance.
Admission must satisfy every applicable ancestor policy and reserve capacity in
the shared budgets. See [key delegation rules](security.md).

Daily capacity uses the UTC admission date. A hold admitted before midnight
retains its reservation in that day's usage when later settled or released; it
does not move to the next day's budget. Settling a reserved transaction does not
charge it twice. Voids, expiry and terminal failures release unused capacity;
partial settlement keeps the committed portion charged. A refund is a new
policy-controlled outgoing operation, not an automatic daily-capacity credit.

Retries keep the business reference stable. Durable admissions reuse their
reservation for identical financial details; different details under the same
identity are denied. Workers and recovery verify durable admission bindings,
without relying on an HTTP request context surviving the queue boundary.

## Independent approval holds

Above the threshold, the server forces an inflight hold and records a pending
approval decision bound to the transaction details. A scheduled commit cannot
supply approval authority, and ordinary commit paths cannot settle a pending
policy hold. The initiating key cannot approve **or reject** its own request.
Its delegation descendants also cannot act as independent approvers: the
initiator cannot manufacture approval authority by minting another key.

Use a separately provisioned approver with explicit `approvals:write`. Keep that
credential out of the agent's environment and enforce the human review process
around it. A different key is an independent server principal, not proof that a
human reviewed the decision; the server cannot distinguish two credentials held
by the same person or compromised process.

| Operation | Endpoint |
| --- | --- |
| Inspect the calling key's effective policy and UTC capacity | `GET /policies/effective` |
| List pending admissions | `GET /approvals` |
| Approve and settle held legs | `POST /approvals/:admission_id/approve` |
| Reject and release held legs | `POST /approvals/:admission_id/reject` |

The initiating key can inspect its pending admissions; approvers can inspect the
pending review queue. Decisions are final and retryable. If a queued hold has not
been persisted yet, the decision operation reports that state and must be retried.
Observe the admission decision and resulting transaction state rather than
treating a pending hold as an applied payment. Direct void/expiry may release
unused funds; that does not authorize settlement.

## MCP identity binding

```sh
make build-mcp
# Inject this scoped credential through the MCP client's secret environment.
export LEDGERFORGE_MCP_API_KEY='<agent key>'
./ledgerforge-mcp --config ./ledgerforge.json --allow-write
```

`--api-key` supplies the same identity explicitly; the environment option avoids
putting the key in a command line. Each bound tool call resolves the key's current
validity and scopes, carries its trusted principal to core admission, and applies
that key's effective restrictions. Read tools remain available with writes off.
No configured key means deployment-level local datasource authority, not a
spending-policy identity: always bind a key for an agent that must be constrained.

Identity-bound MCP adds `ledgerforge_effective_policy`,
`ledgerforge_remaining_capacity`, `ledgerforge_pending_approvals` and, with writes
enabled, `ledgerforge_submit_policy_transaction`. Resources include
`ledgerforge://policy/effective`, `ledgerforge://policy/capacity` and
`ledgerforge://policy/pending-approvals`. `confirm: true` remains an intent check;
MCP does not provide the independent approval decision workflow itself. Use the
HTTP review endpoints with the separate approver credential.

## Threat model and boundaries

Policies protect against an agent submitting disallowed monetary operations
through the supported authenticated HTTP/MCP paths, including attempts to fan
out through split/bulk requests or delegation. PostgreSQL locks and durable
reservations enforce concurrent budget admission; the policy is more than a
client-side estimate. They do not protect against a database administrator, a
master key, an unbound local MCP process, stolen independent approver credentials,
incorrect policy setup, compromised server code or an authorized but economically
wrong business decision. They do not provide currency conversion, a global
FX-valued budget or payment-rail fraud detection.

Use [verification](verification.md), reconciliation, credential isolation and
operational monitoring alongside policies. The
[agent-policy example](../examples/agent-policy/README.md) exercises a limited
transfer, threshold hold, denied self-approval and independent approval. Its
optional delegation probes require the follow-up delegation/ancestor-budget fix;
those probes intentionally fail on the earlier `44eddbc` snapshot.
