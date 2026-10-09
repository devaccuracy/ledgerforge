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

# Agent policy and independent approval (local only)

This executable example uses the authenticated quickstart stack. It provisions a
limited key and a separately provisioned approver, installs a policy, posts under
the threshold, obtains an approval hold, proves self-approval is denied, and
settles through the independent key. It checks unchanged counters after denial,
approval retry idempotency and daily capacity. Keys are revoked when the script
exits; secrets are never printed. Ledger records are retained as normal history.

```sh
./examples/quickstart/start.sh
./examples/agent-policy/run.sh
```

Set `LEDGERFORGE_API_PORT` for both commands if using another local port. Requires
Python 3 and Docker Compose; the API must remain local. A bounded overdraft on a
separate funding counterpart initializes the demo funds, not real payment rails.

The administrator uses `POST /api-keys` with `name`, `owner`, `scopes` and an
RFC3339 `expires_at`. The script deliberately grants the initiating key
`approvals:write` and `api-keys:write` to test authorization against an initiating
principal that possesses those scopes. A production agent should normally have
neither. The independent approver is created by the administrator, not delegated
by the initiating key, and has only `approvals:write`.

The policy is installed through `PUT /policies/:api_key_id`:

```json
{
  "rules": [{
    "currency": "USD",
    "precision": 100,
    "max_transaction": "5000",
    "daily_limit": "10000",
    "approval_above": "2000"
  }],
  "ledger_ids": ["<created ledger_id>"],
  "balance_ids": ["<source balance_id>", "<destination balance_id>"]
}
```

The initiating key submits `1000` units to `POST /transactions` with
`skip_queue: true` and receives `APPLIED`. `5001` units exceeds the cap and must
be denied. A `3000`-unit request returns `INFLIGHT` with its durable
`meta_data.policy_admission_id`. Sending
`POST /approvals/:admission_id/approve` with the initiator must be denied even
though it has the approval scope. The independent key approves the same ID;
retrying that decision must not settle twice. Usage is `4000` with `6000` units
remaining for that UTC day.

## Delegation regression probes

```sh
./examples/agent-policy/run.sh --delegation-probes
```

The probes create a child and grandchild with no local policy, then require:

- The child inherits the parent's `5000`-unit operation cap.
- Parent/child/grandchild admissions share the ancestor's `10000`-unit daily
  allowance. A `4000`-unit admission after shared usage of `7000` is denied even
  though it is below the operation cap and the source has sufficient funds.
- Both a direct child and an indirect descendant are denied when attempting to
  approve or reject the initiator's hold. A separately provisioned approver can
  settle it.
- A sibling delegation cannot approve or reject a child's request. The
  non-delegated root may approve that descendant's hold with explicit approval
  scope, but remains unable to approve its own requests.

After the shared-budget checks reach `9500` units of spend, the administrator
tightens the root's approval threshold to zero. A child's `100`-unit transfer
then requires review: self, descendant and sibling decisions are denied, while
the explicit non-delegated root approves it. Shared spend finishes at `9600`.

Effective policies intersect permissions and limits; a child policy can narrow
them, and deleting that local policy leaves inherited constraints. Keys created
before ancestry persistence are roots: rotate old delegated keys before relying
on family restrictions. See [key operations](../../docs/security.md).

See [agent safety](../../docs/agent-safety.md) for the threat model, policy narrowing,
UTC reservations and MCP identity binding. These tests use independent server
principals; control of two unrelated credentials by one process is not proof of
human review and remains an operational trust boundary.
