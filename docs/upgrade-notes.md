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

Back up the database and retain available audit checkpoint roots under independent
custody before upgrading. If the installed version has no checkpoint export,
record its available chain head with the backup manifest; the first later export
establishes an external anchor from that point, not retroactive external custody.
Run migrations before starting new servers or workers. Stop old servers and
workers during this transition: they do not persist immutable execution controls.
DDL still needs a brief exclusive table lock; use a maintenance window and a
bounded PostgreSQL `lock_timeout`, and retry if busy writers prevent acquiring it.

Migration 1781162340 adds nullable chain versions without updating the transaction
table. A sealed row with a NULL version retains canonical v1. Existing v1/v2
commitments stay verifiable; new seals use v3, which also covers execution controls,
overdraft caps, queue actions and scheduling. Legacy controls are read from the
original INSERT journal; current customer metadata cannot change execution.
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
