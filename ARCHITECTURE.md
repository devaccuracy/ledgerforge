# LedgerForge architecture

LedgerForge uses a small public surface and private implementation packages to
make production integrations stable while allowing the ledger engine to evolve.

## Package layout

| Path | Responsibility |
| --- | --- |
| `/` | Stable public `ledgerforge` package. It is a compatibility facade for applications importing `github.com/devaccuracy/ledgerforge`. |
| `cmd/ledgerforge` | The LedgerForge CLI and process entry point. |
| `cmd/ledgerforge-mcp` | Local stdio MCP server for controlled agent integration. |
| `internal/core` | Ledger orchestration: transaction execution, balances, queues, lineage, and webhook workflows; composition and lifecycle of reconciliation and reindex services. |
| `api` | HTTP transport, request/response models, routing, and authentication middleware. |
| `database` | Persistence interfaces and PostgreSQL implementations. |
| `model` | Shared ledger domain types and validation. |
| `config` | Configuration loading and defaults. |
| `internal/reconciliation` | External uploads, reconciliation execution, matching strategies, progress, and matching rules. Consumes a narrow repository and core-injected batch and side-effect callbacks. |
| `internal/search` | Search transport and indexing, plus per-instance background reindex admission, progress, and cancellation. |
| `tests/architecture` | Parser-based import rules protecting domain services from facade, core, HTTP, and Gin dependencies. |
| `internal/*` | Private infrastructure adapters such as cache, Redis, PostgreSQL connectivity, search, locks, telemetry, and background hooks. |
| `internal/core/sql` | Embedded, ordered database migrations. |
| `tests/loadtest` | Optional load and performance tooling. |

## Dependency rules

- Consumers and entry points import the root `ledgerforge` package, never
  `internal/core`.
- HTTP handlers depend on the public ledger service; the core does not depend
  on HTTP transport packages.
- Database and infrastructure packages depend on domain models, never on API
  handlers or the CLI.
- New shared application code belongs in an existing focused package or a new
  package under `internal/`; do not add implementation files to the repository
  root.
- MCP tools depend on the public LedgerForge service surface and do not access
  persistence internals directly. Write tools are registered only when the
  operator enables them.

## Validation

Run `make verify` before opening a pull request. It checks module consistency,
formatting, static analysis, hermetic tests, and CLI compilation. Service-backed
tests additionally require PostgreSQL and Redis; use `make test-integration`
after starting the compose services.
