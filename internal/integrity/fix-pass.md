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

Integrity fix pass, based on integration `da4b6fb`.

| Review item | Fix | Regression coverage |
| --- | --- | --- |
| Mutable execution controls | Creation stores typed controls in immutable JSONB alongside financial columns. Reads and recovery use those controls; legacy flags come from original INSERT evidence. Metadata updates reject reserved execution keys. | `TestMetadataOnlyKeyCannotChangeQueuedExecution`, `TestAuditExecutionControlsCannotBeChangedThroughMetadata`, existing lifecycle/lineage tests |
| Missing commitments | Canonical v3 includes caps, queue action, scheduling, controls and recovered historical rate. Journal comparison covers every immutable column. V1/v2 remain verifiable. | `TestAuditCommitsExecutionControlsAndCap`, `TestChainV3CoversEveryField`, v1/v2 golden tests, `TestAuditMixedCanonicalVersions` |
| Intent/completion mismatch | Compare exact major and precise amounts; enforce one terminal completion and the intended terminal status. | `TestIntegrityIntentCompletionAmountAndCardinality` |
| False replay failures | Record grouping lineage and economic schema explicitly; include accepted pending commits when checking remainder voids. | `TestIntegrityCurrentEconomicSchemaAndGrouping` (split, bulk, rate 1.25, queued 40 plus void 60, grouped refunds) |
| Main upgrade false tampering | Retain historical rate; record bounded restoration evidence for already-dropped columns. V3 also commits recovered historic rate. | `TestAuditSeededMainAndV1Upgrade`, integration's pinned-main upgrade test |
| Upgrade blocking | Nullable additions avoid backfill/heap rewrites. Constraints start NOT VALID; separate validation and concurrent index migration. | Seeded upgrade checks heap identity and absence of UPDATE backfill; `TestAuditUpgradeValidationDoesNotBlockMetadataWriter` holds a real PostgreSQL writer open during validation |

Validation commands:

```sh
GOFLAGS=-buildvcs=false go build ./...
GOFLAGS=-buildvcs=false go vet ./...
gofmt -l .
GOFLAGS=-buildvcs=false go test -short ./...
/tmp/claude-1005/lfo/itest.sh --down integrity
/tmp/claude-1005/lfo/itest.sh /home/hadi/projects/ledgerforge-wt/integrity integrity -count=1 ./...
/tmp/claude-1005/lfo/itest.sh /home/hadi/projects/ledgerforge-wt/integrity integrity -race -count=1 -run 'TestAudit|TestIntegrity|TestLifecycle|TestChain_' ./internal/core
```

Build, vet and formatting pass. The PostgreSQL integrity, lifecycle, chain and
concurrency tests pass. Both the full fresh-database run and short suite fail
only `api/TestPreciseAmountSafeMessageAllowlist` (backend prefix/suffix cases):
the merged money test expects a generic positive-amount message, while the
merged security implementation returns the safe field-specific message. This
is outside integrity ownership; its code and test are unchanged by this pass.

Upgrade maintenance requirements and intentionally unsupported historic economic
schemas are described in `docs/upgrade-notes.md`.

Surgical files outside the original integrity ownership are listed individually
below. Runtime changes persist or consume immutable controls and lineage;
existing test edits update SQL/flag fixtures for that behavior. The new upgrade
notes file was explicitly requested.

- `api/execution_controls_test.go`
- `api/metadata.go`
- `api/transactions.go`
- `database/metadata.go`
- `database/metadata_test.go`
- `database/transaction.go`
- `database/transaction_coalescing.go`
- `database/transaction_criteria.go`
- `database/transaction_filters.go`
- `database/transaction_grouping.go`
- `database/transaction_lineage.go`
- `database/transaction_queries.go`
- `database/transaction_queue.go`
- `database/transaction_recovery.go`
- `database/transaction_refunds.go`
- `database/transactions_test.go`
- `docs/upgrade-notes.md`
- `internal/core/index_task.go`
- `internal/core/index_task_test.go`
- `internal/core/lineage_credit.go`
- `internal/core/lineage_debit.go`
- `internal/core/lineage_processing.go`
- `internal/core/lineage_shadow.go`
- `internal/core/metadata.go`
- `internal/core/metadata_test.go`
- `internal/core/queue.go`
- `internal/core/queue_enqueue_test.go`
- `internal/core/queue_recovery.go`
- `internal/core/queue_recovery_test.go`
- `internal/core/sql/1780963300.sql`
- `internal/core/sql/1781162340.sql`
- `internal/core/sql/1781162341.sql`
- `internal/core/sql/1791504100.sql`
- `internal/core/sql/1791504101.sql`
- `internal/core/transaction_batch_test.go`
- `internal/core/transaction_bulk.go`
- `internal/core/transaction_coalescing.go`
- `internal/core/transaction_coalescing_coverage_test.go`
- `internal/core/transaction_execution.go`
- `internal/core/transaction_inflight.go`
- `internal/core/transaction_queue.go`
- `internal/core/transaction_refunds.go`
- `internal/core/transaction_rejection.go`
- `internal/core/transaction_test.go`
- `internal/hotpairs/router.go`
- `model/execution_controls.go`
- `model/transaction.go`
