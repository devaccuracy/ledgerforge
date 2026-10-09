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

# Ledger concepts

## Ledgers and balances

A ledger groups balances for your application's accounting model. A balance
has a currency and may be linked to an identity. A transaction moves value
from a source balance to a destination balance; split transactions use multiple
sources or destinations. Model the external funding or settlement counterpart
as a balance too, so value entering a wallet still has a debit and a credit.

Balances use integer units:

```text
balance = credit_balance - debit_balance
inflight_balance = inflight_credit_balance - inflight_debit_balance
```

A transfer increases the source's debit counter and the destination's credit
counter by the same integer amount. Across the balances of a closed system,
total credits equal total debits. A negative balance represents a debit position;
its business meaning depends on the account model you choose.

Spendable source funds normally subtract inflight debit reservations from the
committed balance. Queued debit reservations also participate when queued
checks are enabled. Pending incoming funds are not settled funds. Overdrafts
must be explicit: `allow_overdraft: true` with no positive `overdraft_limit`
permits unbounded overdraft, so set a limit when the account model requires one.

## Precision and rounding

`precision` is a multiplier, not a count of decimal places. With precision `100`,
`precise_amount: 1250` means `12.50`. Use a consistent multiplier for a currency
throughout your integration and send it explicitly when creating transactions.
The API accepts positive integral precision; an omitted transaction precision
ultimately defaults to `1` when no stored precision is available.

`precise_amount` is a JSON integer. Send either it or the compatibility
`amount` field: the create API rejects requests supplying both with a nonzero
`amount`. Balance counters are also JSON integers. Client libraries must
preserve large integers instead of decoding them through a floating-point
number; JavaScript numbers cannot represent every integer above `2^53 - 1`.
Do not quote `precise_amount` as a JSON string with the current API.

For compatibility, `amount` accepts a JSON number. The HTTP API retains its
original decimal token for exact conversion rather than multiplying a decoded
floating-point value. Its decimal value
is multiplied by precision and rounded to the nearest integer unit, with ties
away from zero. For example, `amount: 1.005` at precision `100` becomes `101`
units. A positive amount that rounds to zero units is rejected. Go callers supplying
only a float cannot recover input precision already lost before the call; use
`model.ParseAmount` or an exact integer amount. Prefer `precise_amount` with an
explicit precision for financial integrations.

The compatibility fields `rate` and `currency_multiplier` do not perform ledger
currency conversion. Calculate and account for currency exchange explicitly in
your application; do not assume a transfer applies an FX rate.

## Transactions and references

Submit a transfer to `POST /transactions`, specifying source, destination,
currency, amount units and a unique `reference`. Keep that reference stable for
one business operation. Duplicate references are rejected; use
`GET /transactions/reference/:reference` to recover an existing outcome after a
timeout. A response acknowledging a queued request is not evidence of settlement.

By default, workers process accepted transactions asynchronously. With
`skip_queue: true`, the request waits for execution. Observe the returned
`status` and fetch the stored transaction to establish its outcome:

| State | Meaning |
| --- | --- |
| `QUEUED` | Accepted for worker processing. |
| `SCHEDULED` | Deferred until a scheduled execution time. |
| `INFLIGHT` | Funds reserved pending settlement or release. |
| `APPLIED` | Posted to committed balances. |
| `VOID` | An inflight reservation was released. |
| `REJECTED` | Execution failed validation or another processing constraint. |

Split transfers distribute value among the specified legs. Bulk APIs offer
explicit atomic and asynchronous options; select the required behavior and
inspect their reported outcomes instead of assuming every batch is atomic.

Financial postings are preserved. Descriptive metadata can change. Correct an
applied posting through an additional transaction or the refund API so the
history retains the original operation and its reversal.

## Inflight transactions

Set `inflight: true` to reserve funds without applying a committed transfer.
Both balances track inflight components, and the source's hold reduces its
available funds. Update the reservation through
`PUT /transactions/inflight/:txID` with `status: "commit"` or `status: "void"`
to commit or release it. Commits may be partial;
voiding releases the remaining hold. Optional expiry and commit dates schedule
release or settlement through workers, so keep workers running.

Inflight is a funds reservation mechanism. It does not by itself prove that a
human approved the transaction or give an agent a spending limit.

## Refunds and previews

`POST /refund-transaction/:id` creates a linked reversal with the original source
and destination exchanged. It preserves the original posting and exact amount.
Refunds apply only to `APPLIED` transactions. `QUEUED`, `SCHEDULED`,
`INFLIGHT`, `VOID` and `REJECTED` originals are not refundable. A durable claim
prevents concurrent retries from producing multiple reversals. Refunds queue by default;
request `skip_queue: true` for synchronous execution.

Transaction, refund and inflight APIs support `dry_run: true`. A preview projects
balance effects without persisting a posting, consuming its reference or
sending a webhook. It is an observation of current state, not a reservation or
a guarantee that a later request will succeed under concurrent activity.

Policies apply to admitted transfers, including split and bulk legs and
refunds; a refund is a new admitted operation, not a daily-budget credit. See
[agent safety](agent-safety.md) for approval and capacity semantics.

See the [balanced transfer example](../examples/quickstart/README.md) and
[deployment guide](deployment.md).
