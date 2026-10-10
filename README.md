<p align="center">
  <a href="https://ledgerforge.io">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="docs/assets/wordmark-dark.png">
      <img src="docs/assets/wordmark-light.png" alt="LedgerForge" width="300">
    </picture>
  </a>
</p>

<h1 align="center">The ledger for AI-native money movement</h1>

<p align="center">
  The system of record for fintech wallets, payouts, marketplaces and billing —
  with spending limits that keep AI agents in check and an audit trail you can prove.
</p>

<p align="center">
  <a href="https://github.com/devaccuracy/ledgerforge/releases/latest"><img alt="Latest release" src="https://img.shields.io/github/v/release/devaccuracy/ledgerforge?sort=semver"></a>
  <a href="https://github.com/devaccuracy/ledgerforge/actions/workflows/go.yml"><img alt="Build and Test" src="https://github.com/devaccuracy/ledgerforge/actions/workflows/go.yml/badge.svg?branch=main"></a>
  <a href="LICENSE.md"><img alt="License: Apache 2.0" src="https://img.shields.io/github/license/devaccuracy/ledgerforge"></a>
  <a href="go.mod"><img alt="Go 1.26.9 or later" src="https://img.shields.io/badge/Go-1.26.9%2B-00ADD8?logo=go&amp;logoColor=white"></a>
  <a href="https://pkg.go.dev/github.com/devaccuracy/ledgerforge"><img alt="Go Reference" src="https://pkg.go.dev/badge/github.com/devaccuracy/ledgerforge.svg"></a>
</p>

<p align="center">
  <a href="https://ledgerforge.io"><strong>Website</strong></a> ·
  <a href="https://ledgerforge.io/docs"><strong>Docs</strong></a> ·
  <a href="https://ledgerforge.io/cloud"><strong>Cloud</strong></a> ·
  <a href="CHANGELOG.md"><strong>Changelog</strong></a> ·
  <a href="ROADMAP.md"><strong>Roadmap</strong></a>
</p>

<p align="center">
  <a href="#open-source-and-ledgerforge-cloud">Open-source core (Apache 2.0)</a> ·
  Managed with <a href="https://ledgerforge.io/cloud">LedgerForge Cloud</a> ·
  <a href="https://ledgerforge.io/contact/us?interest=enterprise">Enterprise</a>
</p>

LedgerForge is an open-source double-entry ledger. It records wallet transfers,
payouts, billing activity, reservations and refunds in exact integer units,
backs that history with a verifiable audit chain, and enforces spending
policies on every API key, including the keys your AI agents use. Use it
through an HTTP API, a Go library or a local MCP server.

PostgreSQL stores the financial records and Redis runs queues and background
workers; search and observability services are optional. LedgerForge records
money movement. Connecting payment rails and defining your accounting model
remain application responsibilities.

## Use cases

- **Wallets and stored value:** a balance per customer or account, inflight
  holds that reduce spendable funds, bounded overdrafts and linked refunds.
- **Payouts and disbursements:** reserve funds while a payout is in flight,
  commit or void the reservation when the rail reports back, and reconcile
  against external records.
- **Marketplaces:** split one payment across seller, fee and platform balances
  with deterministic allocation, so the legs always sum to the total.
- **Usage-based billing:** record metered charges in exact units, queue or
  schedule them, and retry safely with idempotent references.
- **AI agents moving money:** give each agent its own API key with currency
  limits, allowlists, daily budgets and approval holds that the server
  enforces, and connect agents through the [MCP server](docs/mcp.md).

## Why LedgerForge

- **Exact money:** integer postings, explicit precision and deterministic
  splits; see the [accounting guarantees](#accounting-guarantees).
- **A verifiable ledger:** read-only `ledgerforge verify` replays accounting
  evidence. Versioned audit chains and exportable checkpoints support
  `audit verify --anchor` against independently retained roots. See the
  [verification guide](docs/verification.md) for coverage and legacy limits.
- **Agent guardrails:** per-key currency and precision limits, balance and
  ledger allowlists, durable daily reservations and approval holds, enforced by
  the server rather than the client. Delegated keys inherit restrictions and
  share ancestor budgets. Bind MCP to an API key and keep independent approver
  credentials outside the agent's authority. See
  [agent safety](docs/agent-safety.md) and the
  [executable policy example](examples/agent-policy/README.md).
- **A hardened API:** owner-bound key management, constrained delegation,
  authoritative revocation checks, bounded input and sanitized backend errors.
  Dedicated webhook signing secrets and destination controls protect outbound
  integrations. See [security operations](docs/security.md).
- **Tested invariants:** exact-amount and allocation fuzz targets, generated
  PostgreSQL operation sequences, balance conservation checks and race tests
  for financial lifecycle and spending-capacity transitions.
- **Operational workflows:** inflight reservations, partial settlement,
  refunds, split and bulk transfers, reconciliation, lineage, snapshots and
  optional search, metrics and OpenTelemetry.
- **Traceable releases:** binaries for Linux, macOS and Windows, multi-platform
  images, checksums, SPDX SBOMs and image attestations, published only after
  the test, vulnerability and fuzz gates pass.

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

Requires Docker with Compose and Python 3:

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
The [example guide](examples/quickstart/README.md) covers the API payload,
credentials and cleanup. These credentials and database defaults are for local
use only; read the [deployment guide](docs/deployment.md) before exposing a
service.

## Install

- **Release binaries:** download `ledgerforge` and `ledgerforge-mcp` for Linux,
  macOS or Windows from [GitHub Releases](https://github.com/devaccuracy/ledgerforge/releases),
  then check the archive against `checksums.txt` (`sha256sum -c checksums.txt --ignore-missing`).
- **Container image:** `docker pull ghcr.io/devaccuracy/ledgerforge:1.0.0`. The
  image contains both binaries; the release lists its digest and SBOMs.
- **From source:** `go install github.com/devaccuracy/ledgerforge/cmd/ledgerforge@v1.0.0`
  (Go 1.26.9 or later).

Run `ledgerforge version` to confirm what you installed, then follow the
[deployment guide](docs/deployment.md) and run `ledgerforge migrate up` before
starting the server. Before upgrading an existing database, read the
[integrity upgrade notes](docs/upgrade-notes.md).

### Run the published image locally

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

## Open source and LedgerForge Cloud

Everything in this repository is free and open source under
[Apache 2.0](LICENSE.md): the ledger, verification and audit tooling, agent
spending policies and approvals, the MCP server and the release artifacts.
There are no license keys or feature gates, and you can run it in production
yourself.

LedgerForge Cloud is a service, not different software: it operates the same
open-source core for you.

| | Open source | LedgerForge Cloud | Enterprise |
| --- | --- | --- | --- |
| What it is | The software in this repository | Managed LedgerForge deployments, operated per customer | Contracted deployment, support and review services |
| Ledger, verification, agent policies, MCP server | Included | Same open-source core | Same open-source core |
| Who operates it | You | The LedgerForge team (dedicated infrastructure per customer) | Dedicated or in-VPC deployment assistance |
| Upgrades, backups and monitoring | You | Operated for you | Migration and integration assistance |
| Verification and audit checkpoints | You run `ledgerforge verify` and `audit export` | Scheduled `ledgerforge verify` runs with audit checkpoints exported and anchored for each deployment | Architecture and audit reviews |
| Support | Community, through GitHub | Onboarding and support | SLA and support plan agreed per contract; priority security response |
| Hosted multi-tenant service with a dashboard | — | Planned ([roadmap](ROADMAP.md)) | — |
| Price | Free | Request access | Contact sales |
| Get started | [Quickstart](#quickstart-a-balanced-transfer) | [Request access](https://ledgerforge.io/cloud) | [Contact sales](https://ledgerforge.io/contact/us?interest=enterprise) |

## Documentation

The [documentation index](docs/README.md) lists every guide. Start with:

- [Concepts](docs/concepts.md): amounts, balances and transaction states
- [Deployment](docs/deployment.md): secrets, TLS, limits and webhook verification
- [Verification](docs/verification.md): accounting replay and external audit anchoring
- [Agent safety](docs/agent-safety.md): spending policies and independent approval
- [Security operations](docs/security.md): API keys, delegation and rotation
- [MCP server](docs/mcp.md): connect AI agents to the ledger
- [Architecture](ARCHITECTURE.md): package layout and trust boundaries

Online documentation is at [ledgerforge.io/docs](https://ledgerforge.io/docs).
The [roadmap](ROADMAP.md) describes what is planned and the
[changelog](CHANGELOG.md) records what has shipped.

## Development

```sh
make verify
make test-integration
make vuln
make cover
```

[CONTRIBUTING.md](CONTRIBUTING.md) covers prerequisites, isolated integration
tests and pull request expectations.

## Community, support and license

- **Bugs and proposals:** use the
  [issue forms](https://github.com/devaccuracy/ledgerforge/issues/new/choose).
- **Questions:** start with the [documentation](docs/README.md), then open a
  [question](https://github.com/devaccuracy/ledgerforge/issues/new?template=question.yml)
  if it does not help. [SUPPORT.md](SUPPORT.md) covers community and
  commercial support.
- **Contributing:** read [CONTRIBUTING.md](CONTRIBUTING.md) and follow the
  [Code of Conduct](CODE_OF_CONDUCT.md). [GOVERNANCE.md](GOVERNANCE.md) explains
  how decisions are made.
- **Security:** report vulnerabilities privately to
  [security@ledgerforge.io](mailto:security@ledgerforge.io); see the
  [security policy](SECURITY.md).

Licensed under [Apache 2.0](LICENSE.md). Required notices are in [NOTICE.md](NOTICE.md).
