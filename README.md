# LedgerForge

[![Build and Test Status](https://github.com/devaccuracy/ledgerforge/actions/workflows/go.yml/badge.svg)](https://github.com/devaccuracy/ledgerforge/actions/workflows/go.yml)
[![Docker Build Status](https://github.com/devaccuracy/ledgerforge/actions/workflows/docker-publish.yml/badge.svg)](https://github.com/devaccuracy/ledgerforge/actions/workflows/docker-publish.yml)
[![Linter Status](https://github.com/devaccuracy/ledgerforge/actions/workflows/lint.yml/badge.svg)](https://github.com/devaccuracy/ledgerforge/actions/workflows/lint.yml)
[![Contributor Covenant](https://img.shields.io/badge/Contributor%20Covenant-2.1-4baaaa.svg)](CODE_OF_CONDUCT.md)

LedgerForge is a financial infrastructure platform building production-grade, open-source ledger systems for teams that move money.

LedgerForge Core is a high-performance double-entry ledger for developers building wallets, billing systems, reconciliation workflows, and money-movement infrastructure with correctness, traceability, and operational control.

Learn more at [ledgerforge.io](https://ledgerforge.io).

## Links

- Website: [ledgerforge.io](https://ledgerforge.io)
- GitHub: [github.com/devaccuracy/ledgerforge](https://github.com/devaccuracy/ledgerforge)
- Security reports: [security@ledgerforge.io](mailto:security@ledgerforge.io)

## Status

This repository contains LedgerForge Core, the open-source ledger engine for self-hosted deployments.

LedgerForge is built for teams evaluating ledger infrastructure locally, then moving toward production deployment when workload, security, and operating requirements are clear. Managed deployment and hosted operations workflows are handled through private onboarding.

The repository preserves upstream Git history and retains the required Apache License 2.0 attribution documented in `NOTICE.md`.

## Installation

```bash
git clone https://github.com/devaccuracy/ledgerforge.git
cd ledgerforge
cp .env.example .env
cp ledgerforge.example.json ledgerforge.json
```

## Example configuration

```json
{
  "project_name": "LedgerForge",
  "data_source": {
    "dns": "postgres://postgres:password@postgres:5432/ledgerforge?sslmode=disable"
  },
  "redis": {
    "dns": "redis:6379"
  },
  "server": {
    "port": "5001"
  }
}
```

## Running locally

Start the bundled services and LedgerForge containers:

```bash
docker compose up
```

Build and run the CLI directly:

```bash
go build -o ledgerforge ./cmd/*.go
./ledgerforge --help
```

Run database migrations:

```bash
./ledgerforge migrate up
```

## What LedgerForge Core Provides

- A double-entry ledger for balances, ledgers, transactions, inflight transactions, scheduled transactions, overdrafts, and historical balance snapshots.
- Multi-currency balance tracking for accounts and wallets.
- Transaction workflows including split transactions, bulk transactions, and two-phase commit-style inflight flows.
- Reconciliation primitives for matching external records against internal ledger activity.
- Identity and account models for linking financial entities to balances and transaction flows.
- Webhook, queue, search, metrics, and operational hooks for production financial systems.
- PostgreSQL-backed persistence with Redis-backed queues and operational workers.

## Deployment Options

- **Self-hosted Core:** run LedgerForge locally or on your own infrastructure with Docker, PostgreSQL, and Redis.
- **Managed production planning:** request private onboarding for workload review, Kubernetes or private-cloud planning, observability, backups, and operating runbooks.

LedgerForge is API-first. Teams can run the open-source Core in their own environment and integrate through the HTTP API or wrap ledger workflows in their own services.

## Development

```bash
go test -short ./...
make build
docker compose config
```

`go test -short ./...` covers the hermetic fast path. Service-backed integration tests expect local PostgreSQL and Redis, which you can start with `docker compose up`.

## Origin And Attribution

LedgerForge is derived from an upstream Apache License 2.0 project.

Original Git history, commit authorship, and license notices are preserved for transparency, attribution, and continuity. Additional origin details are documented in `NOTICE.md`.

LedgerForge Core is maintained as the open-source foundation of LedgerForge's financial infrastructure platform.

## Contributing

Contributions and feedback are welcome. Please read [CONTRIBUTING.md](CONTRIBUTING.md) and [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) before opening issues or pull requests.

## Security

LedgerForge is financial infrastructure software. Please do not open public issues for suspected vulnerabilities. Report security concerns privately to [security@ledgerforge.io](mailto:security@ledgerforge.io). See [SECURITY.md](SECURITY.md).

## License

LedgerForge is distributed under the Apache License 2.0. Original upstream copyright and license notices are retained where applicable.
