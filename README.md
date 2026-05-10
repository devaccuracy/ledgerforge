# LedgerForge

[![Build and Test Status](https://github.com/devaccuracy/ledgerforge/actions/workflows/go.yml/badge.svg)](https://github.com/devaccuracy/ledgerforge/actions/workflows/go.yml)
[![Docker Build Status](https://github.com/devaccuracy/ledgerforge/actions/workflows/docker-publish.yml/badge.svg)](https://github.com/devaccuracy/ledgerforge/actions/workflows/docker-publish.yml)
[![Linter Status](https://github.com/devaccuracy/ledgerforge/actions/workflows/lint.yml/badge.svg)](https://github.com/devaccuracy/ledgerforge/actions/workflows/lint.yml)
[![Contributor Covenant](https://img.shields.io/badge/Contributor%20Covenant-2.1-4baaaa.svg)](CODE_OF_CONDUCT.md)

LedgerForge is an open-source double-entry ledger and financial core for developers building reliable fintech, wallet, billing, reconciliation, and money-movement systems.

## Status

LedgerForge is an independent open-source ledger project. This repository preserves the upstream Git history and retains the required Apache License 2.0 attribution documented in `NOTICE.md`.

LedgerForge documentation will be published as the project evolves. Until then, the repository README, examples, tests, and source documentation are the primary reference.

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

## What LedgerForge Provides

- A double-entry ledger for balances, ledgers, transactions, inflight transactions, scheduled transactions, overdrafts, and historical balance snapshots.
- Reconciliation primitives for matching external records against internal ledger activity.
- Identity and account models for linking financial entities to balances and transaction flows.
- Webhook, queue, search, metrics, and operational hooks for production financial systems.

## Development

```bash
go test -short ./...
make build
docker compose config
```

`go test -short ./...` covers the hermetic fast path. Service-backed integration tests expect local PostgreSQL and Redis, which you can start with `docker compose up`.

## Origin

LedgerForge is derived from an upstream Apache License 2.0 project.

Original Git history, commit authorship, and license notices are preserved for transparency, attribution, and continuity. Additional origin details are documented in `NOTICE.md`.

LedgerForge is now maintained as an independent open-source ledger project.

## Contributing

Contributions and feedback are welcome. Please read [CONTRIBUTING.md](CONTRIBUTING.md) and [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) before opening issues or pull requests.

## License

LedgerForge is distributed under the Apache License 2.0. Original upstream copyright and license notices are retained where applicable.
