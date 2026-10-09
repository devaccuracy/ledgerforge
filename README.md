# LedgerForge

[![Build and Test](https://github.com/devaccuracy/ledgerforge/actions/workflows/go.yml/badge.svg)](https://github.com/devaccuracy/ledgerforge/actions/workflows/go.yml)

LedgerForge is an open-source double-entry ledger for applications that move
money. Use it to record wallet transfers, billing activity, reservations and
refunds through an HTTP API, a Go library or a local MCP server.

PostgreSQL stores the financial records. Redis supports queues and background
workers. Search and observability services are optional. LedgerForge records
money movement; connecting payment rails and defining your accounting model
remain application responsibilities.

## Accounting guarantees

- **Exact amounts:** postings and balance arithmetic use integer units. Supply
  `precise_amount` with an explicit `precision` to avoid floating-point input
  loss; `1250` at precision `100` represents `12.50`.
- **Double entry:** each applied transfer debits its source and credits its
  destination by the same amount. Split transfers distribute that amount
  across their legs.
- **Immutable postings:** corrections and refunds create linked transactions.
  Financial postings and accepted execution controls are preserved; customer
  metadata can be updated.
- **Idempotency by reference:** reuse the same unique transaction `reference`
  after a timeout. Duplicate references are rejected; look up the original
  outcome instead of retrying with a new reference.

See [ledger concepts](docs/concepts.md) for amount handling, balance semantics
and asynchronous transaction states.

## Quickstart: a balanced transfer

Requires Docker with Compose and Python 3. From a checkout:

```sh
git clone https://github.com/devaccuracy/ledgerforge.git
cd ledgerforge
./examples/quickstart/start.sh
./examples/quickstart/transfer.sh
```

This **local-only demo** builds the current checkout, generates a random master
key, runs migrations and binds the API to `127.0.0.1:5001`. It creates two USD
balances and posts `12.50` synchronously using integer units. The source gets a
bounded demo overdraft of `12.50`; the result is `-1250` and `1250` units, with
equal total debits and credits. A retry with the same reference must leave
those balances unchanged.

```text
source: -1250 units
destination: 1250 units
total debits = total credits = 1250 units
reference retry: balances unchanged
```

Stop it with `docker compose -p ledgerforge-demo -f examples/quickstart/compose.yaml down`.
See the [example guide](examples/quickstart/README.md) for the API payload,
credentials and cleanup. These credentials and database defaults are for local
use. Read the [deployment guide](docs/deployment.md) before exposing a service.

## Why LedgerForge

- **Exact money:** integer postings and balance counters, preserved decimal
  request tokens, explicit precision and deterministic split allocation. Invalid
  monetary inputs are rejected before posting.
- **A verifiable ledger:** read-only `ledgerforge verify` replays accounting
  evidence. Versioned audit chains and exportable checkpoints support
  `audit verify --anchor` against independently retained roots. See the
  [verification guide](docs/verification.md) for coverage and legacy limits.
- **Agent guardrails:** per-key currency/precision limits, balance/ledger
  allowlists, durable daily reservations and approval holds enforced by the
  server. Delegated keys inherit restrictions and share ancestor budgets.
  Bind MCP to an API key and keep independent approver credentials
  outside the agent's authority. See [agent safety](docs/agent-safety.md) and the
  [executable policy example](examples/agent-policy/README.md).
- **A hardened API:** owner-bound key management, constrained delegation,
  authoritative revocation checks, bounded input and sanitized backend errors.
  Dedicated webhook signing secrets and destination controls protect outbound
  integrations. See [security operations](docs/security.md).
- **Tested invariants:** exact-amount and allocation fuzz targets, generated
  PostgreSQL operation sequences, balance conservation checks and race tests for
  financial lifecycle and spending-capacity transitions.
- **Operational workflows:** inflight reservations, partial settlement, refunds,
  split/bulk transfers, reconciliation, lineage, snapshots and optional search,
  metrics and OpenTelemetry. PostgreSQL retains financial truth while workers
  handle asynchronous execution and delivery.

## Run the published image locally

```sh
cp ledgerforge.example.json ledgerforge.json
# Replace server.secret_key with a random secret before starting.
# For example, generate one with: openssl rand -hex 32
docker compose up -d
```

The config explicitly enables authentication. API requests use the
`X-LedgerForge-Key` header. Keep `ledgerforge.json` private and out of version
control. The `:main` image tracks tested main-branch commits; pin a release tag
or digest in production with `LEDGERFORGE_IMAGE`.

## Documentation

- [Concepts: amounts, balances and transaction states](docs/concepts.md)
- [Deployment: secrets, TLS, limits and webhook verification](docs/deployment.md)
- [Integrity upgrade maintenance and historical evidence](docs/upgrade-notes.md)
- [Accounting verification and external audit anchoring](docs/verification.md)
- [Agent spending policies and independent approval](docs/agent-safety.md)
- [API security, delegation and rotation](docs/security.md)
- [Architecture and trust boundaries](ARCHITECTURE.md)
- [MCP server](docs/mcp.md)
- [Metrics and monitoring](docs/metrics.md)
- [Metadata webhooks](docs/metadata-webhooks.md)
- [Contributing and isolated integration tests](CONTRIBUTING.md)
- [Changelog](CHANGELOG.md) and [security policy](SECURITY.md)
- [Online documentation](https://ledgerforge.io/docs)

## Development

```sh
make verify
make test-integration
make vuln
make cover
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for prerequisites and test isolation.

## Community and license

Use [GitHub issues](https://github.com/devaccuracy/ledgerforge/issues) for bugs
and feature requests. Follow the [Code of Conduct](CODE_OF_CONDUCT.md).
Report vulnerabilities privately to [security@ledgerforge.io](mailto:security@ledgerforge.io).

Licensed under [Apache 2.0](LICENSE.md). Required notices are in [NOTICE.md](NOTICE.md).
