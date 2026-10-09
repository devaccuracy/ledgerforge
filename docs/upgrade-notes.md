<!-- Copyright 2026 LedgerForge Contributors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License. -->

# Integrity upgrade maintenance

Supported paths are a fresh installation or an upgrade from main (`feda9b1`) or
an earlier published release. Those versions have no transaction hash chain;
this release introduces canonical v3 directly. The intermediate `44eddbc` v2
migration set was unreleased and is outside the supported upgrade paths.

Back up the database before upgrading. The first checkpoint export establishes
an external anchor from that point, not retroactive external custody. Keep later
checkpoint exports under independent custody.
Run migrations before starting new servers or workers. Stop old servers and
workers during this transition: they do not persist immutable execution controls.
DDL still needs a brief exclusive table lock; use a maintenance window and a
bounded PostgreSQL `lock_timeout`, and retry if busy writers prevent acquiring it.

Migration 1781162340 adds nullable chain versions without updating the transaction
table. The worker creates only v3 seals, including for historical transactions.
The verifier retains v1/v2 format decoders for explicitly supplied records and
test fixtures; a NULL version on such a sealed record denotes v1. These decoders
do not provide migration compatibility for an unreleased v1/v2 installation.
V3 covers execution controls, overdraft caps, queue actions and scheduling.
Legacy controls are read from the original INSERT journal using historical type
checks: nonboolean flag values mean false, and invalid string controls use their
original defaults. Raw evidence and canonical bytes are preserved; current
schema controls remain strictly typed. Customer metadata cannot change execution.
Historical non-unit rates without a known economic schema remain unsupported by
financial replay. New postings record economic schema 1, in which rate is ignored.

Migration 1781162341 creates the journal lookup index CONCURRENTLY outside a DDL
transaction, then validates the chain constraint separately. Migration 1791504100
adds nullable execution controls without a data rewrite; 1791504101 validates its
constraint separately. Concurrent index creation and validation still scan tables
and consume I/O: budget time and disk space for large installations. PostgreSQL
12+ implements constant column defaults without rewriting existing rows.
If a concurrent index build is interrupted, inspect `pg_index.indisvalid`, drop
the invalid index concurrently, and rerun migrations before starting workers.

The historical rate column is retained as evidence. Installations that already
ran the rate-dropping migration get a recorded, bounded schema transition: the
original INSERT journal remains intact, and only its pre-upgrade rate versus the
restored default is tolerated. Later rate mutations still fail audit verification.
Run `ledgerforge verify` and `ledgerforge audit verify` after upgrading; these are
read-only and report unsupported legacy rows and pending chain coverage separately.
