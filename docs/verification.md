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

# Verify ledger history

LedgerForge has two independent checks. Accounting replay tests whether stored
balances agree with persisted financial operations. Audit verification tests
whether sealed transaction commitments, their links and checkpoint roots agree
with the current database evidence. Run both; neither proves that a real-world
payment happened or that an application chose the correct business transaction.

## Read-only accounting replay

```sh
./ledgerforge --config ./ledgerforge.json verify --json > replay-report.json
```

`ledgerforge verify` reads balances and transaction history in one read-only
PostgreSQL `REPEATABLE READ` snapshot. It replays integer debits, credits,
inflight reservations and lifecycle releases, then compares derived counters
with stored counters. Checks include invalid amounts/scales, missing balances,
malformed parent/child relationships, reversal consistency and conservation in
compatible accounting components. Terminal completions must match their
persisted intent's amounts, endpoints, currency and precision; duplicate terminal
completions are reported. Immutable execution evidence distinguishes grouping
parents from economic lineage. Pending accepted inflight commits participate in
reservation bounds, including when a remainder has already been voided.
Queued and scheduled intents do not count as settled economic legs. The command
never repairs balances or calls reconciliation triggers. It exits nonzero when its report has issues.

The JSON fields are `verified`, `balances`, `transactions` and `issues`.
Each issue supplies a kind and, where applicable, a transaction/balance ID,
field, expected value and actual value. Preserve the report and database
snapshot when investigating; do not rewrite postings merely to make it pass.

Replay establishes internal consistency for the supported evidence in that
snapshot. It cannot detect a fully rewritten but mutually consistent database,
prove application authorization, verify customer intent, confirm bank activity,
or recover an opening position omitted from history. Use external reconciliation
and independently retained audit checkpoints for those separate concerns.

## Versioned hash chains and checkpoints

Enable sealing in the config used by the API server:

```json
{
  "transaction": {
    "hash_chain": { "enabled": true }
  }
}
```

The feature is disabled by default. Start or restart `ledgerforge start` with
that config; the API server owns the hash-chain processor. Queue workers alone
do not seal the audit chain. The default poll interval is five seconds and the
trailing delay is thirty seconds; recent rows will therefore remain unsealed temporarily. Redis and PostgreSQL
must remain available to the server process.

Canonical v3 uses a domain identifier, version and length-delimited fields hashed
with SHA-256. Its embedded v2 payload includes
the previous hash, database and transaction IDs, source/destination, decimal and precise amounts, precision, retained rate,
currency, status, reference, parent, effective date and creation time. Numeric
values come from PostgreSQL text without a floating-point round trip.

V3 additionally commits the overdraft cap, queue action, immutable execution controls, scheduled
time and original historical rate evidence. The controls include execution
flags, grouping/lineage decisions and inflight expiry/commit dates. Mutable
customer metadata is excluded; reserved execution metadata is part of the
immutable evidence. The worker creates only v3 seals. A sealed
commitment cannot be changed through ordinary transaction updates.

Each sealed batch records a checkpoint with `sequence`, `count`, `head_hash`
and `created_at`. Sequence is the order in which the audit worker observed rows,
not a guarantee of ID, execution-time or PostgreSQL commit order. A late commit
can be sealed later even if its database ID is lower. The chain-state lock
serializes concurrent chainers.

```sh
./ledgerforge --config ./ledgerforge.json audit verify --json > audit-report.json
./ledgerforge --config ./ledgerforge.json audit export > checkpoints.json
```

`verify-chain` remains a compatible alias for `audit verify`. The verifier reads
head, links, checkpoints and coverage in one read-only repeatable snapshot. It
checks contiguous sequences, previous hashes, recomputed canonical hashes,
checkpoint counts/roots and the stored chain head. INSERT-journal evidence can
also identify deleted or changed financial rows before they are sealed.

## Pending coverage versus tampering

The audit report separates `pending` from `tampering`:

| Report | Meaning and response |
| --- | --- |
| `verified: true`, `pending: 0`, empty `tampering` | The observed commitments are consistent and all transaction rows in this snapshot are covered. |
| `verified: true`, `pending > 0`, empty `tampering` | Chain links are intact; some persisted transaction rows are not sealed yet. Check server configuration, trailing delay and backlog. |
| `verified: false`, nonempty `tampering` | The verifier found conflicting or missing evidence. Preserve the database/report and investigate before taking corrective action. |

The CLI exits nonzero for pending coverage too, with a distinct message that
coverage is pending. Do not classify that exit status alone as tampering. Busy
systems may always have recent unsealed rows; establish a cutoff and a bounded
sealing-lag objective. `pending` includes unsealed transaction rows, not just
financially settled transfers, and is unrelated to policy pending approvals.

## External anchoring runbook

Database-only hashes cannot protect against an administrator who can rewrite
both postings and their evidence. Retain checkpoint roots under an independent
trust boundary:

1. Run replay and `audit verify`. Explain any legacy findings or pending coverage;
   an export alone does not establish a clean ledger.
2. Export `checkpoints.json`. Confirm it is a nonempty JSON array and record the
   ledger identity, release/commit, UTC export time and last covered sequence.
   A ledger with no sealed history cannot supply an anchor yet.
3. Sign the exact export bytes with a separate signing identity, or submit their
   digest to an external append-only timestamp/anchoring service. Keep the
   verification key and submission receipt independently.
4. Store the export, signature/receipt and manifest in immutable storage with
   credentials separate from database administrators. Retain prior roots instead
   of replacing each export. Back up the signing/verification procedure too.
5. Periodically retrieve an earlier retained export, authenticate it outside
   LedgerForge, and compare those roots with the live database:

```sh
# Example external signature validation; use your organization's signing system.
gpg --verify checkpoints.json.asc checkpoints.json
./ledgerforge --config ./ledgerforge.json audit verify --anchor checkpoints.json --json
```

`--anchor` accepts the JSON array emitted by `audit export`. It compares retained
checkpoint sequences/counts/roots to the database. It does **not** authenticate a
signature, contact an anchoring service or decide whether the supplied file is
trusted. Signature validation and storage access controls are operator duties.
An anchor protects the prefix it covers; activity after that root still needs
subsequent coverage and exports.

For an incident, suspend destructive administrative operations, retain a
consistent snapshot, retrieve trusted roots and reproduce both checks against
the snapshot. A missing externally retained root is evidence to investigate;
do not replace the anchor with the current database export to clear the finding.

## Legacy and schema limitations

The supported installation paths are fresh databases and upgrades from main
(`feda9b1`) or earlier published releases, which have no transaction hash chain.
This release introduces v3 directly; the intermediate `44eddbc` v2 migration set
was unreleased and is outside the supported upgrade paths. V1/v2 format decoders
remain available for explicitly supplied older-format records and test fixtures,
not as migration compatibility for unreleased installations. V1 omitted precision
and economic parent/effective-date fields; v2 omitted the controls added in v3.
Verification of those formats does not add v3's coverage to their commitments.

New postings identify economic schema 1: retained `rate` is economically ignored
and both legs use the same exact units. Historical non-unit rates without that
evidence remain ambiguous. Replay returns `unsupported_legacy` rather than
guessing their credit semantics, missing parent history or fractional/missing
precise amounts. A finding is not automatically proof of malicious change.

Legacy execution controls are read from original INSERT-journal evidence rather
than current customer metadata. Historical type checks ignore customer values of
the wrong type without changing raw evidence or canonical bytes. Current schema
controls remain strictly typed. Upgrades preserve historical rate evidence; a
recorded schema transition can account for the original rate versus its restored
default when an older installation already ran the rate-dropping migration.
That bounded exception does not authorize later rate mutations. See
[integrity upgrade maintenance](upgrade-notes.md) before migrating seeded history.

Preserve migration evidence and independently retained roots, and review the
canonical version's field coverage before making an integrity claim. Test
supported upgrades on seeded historical rows and customer metadata.
Neither checker repairs missing evidence or certifies history predating retained
records. See [deployment](deployment.md) for migration and backup operations.
