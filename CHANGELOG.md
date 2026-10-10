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

# Changelog

All notable changes to LedgerForge are documented here. This file follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and release tags use
semantic versions.

## [Unreleased]

### Documentation

- Reworked the README with use cases, an open-source and LedgerForge Cloud
  comparison and links to the website, documentation, changelog and roadmap.
- Added `ROADMAP.md`, `SUPPORT.md`, `GOVERNANCE.md` and a documentation index
  at `docs/README.md`.
- Replaced the Markdown issue templates with bug report and feature request
  forms, added contact links for security reports, documentation and support,
  and added `CODEOWNERS`.
- `SECURITY.md` now names 1.0.x as the supported release line.
- Added a pull request checklist, a README wordmark and a repository social
  preview image under `docs/assets/`.

## [1.0.0] - 2026-10-10

The first LedgerForge release: a double-entry ledger with exact integer money
handling, durable transaction lifecycles, read-only replay verification and a
versioned audit chain, server-enforced spending policies and approval holds for
API keys and AI agents, and a hardened HTTP API. Upgrades are supported from
fresh installs and from the previous `main` schema; see the upgrade notes.

### Added

- `ledgerforge version` and `--version` report the release version, commit and
  build date stamped into release binaries and container images.

- `model.ParseAmount` and checked exact amount conversion, raw decimal-token
  preservation for HTTP transaction/inflight requests, and amount/distribution
  fuzz tests with independent arithmetic oracles.
- Read-only `ledgerforge verify` accounting replay with JSON issue reports and
  repeatable-read PostgreSQL snapshots of balances and lifecycle evidence.
- Canonical v3 hash chains, persistent checkpoint roots, `ledgerforge audit verify`,
  `audit export`, external
  `--anchor` comparison, and separate pending-coverage/tampering reports.
- Generated PostgreSQL operation-sequence tests, concurrent snapshot/chainer
  tests, and property tests for mutation, deletion, truncation and retained roots.
- Seeded historical-main migration fixtures and no-op rerun checks, with lifecycle
  rollback tests targeting the intended migration by ID.
- Per-key currency/precision spending rules, aggregate transaction caps, ledger
  and balance allowlists, UTC daily capacity, durable admissions and reservations.
- Policy approval holds, independent approval/rejection endpoints, effective
  policy/capacity inspection and persistence checks for queued execution.
- Immutable API-key delegation ancestry, intersected inherited policies, shared
  ancestor budgets, per-budget capacity reports and approval-family checks.
- Immutable transaction execution controls, economic schema and grouping evidence;
  v3 audit commitments include those controls, overdraft caps, queue actions,
  scheduling and original historical rate evidence.
- MCP API-key identity binding through `--api-key` / `LEDGERFORGE_MCP_API_KEY`,
  policy/capacity/approval tools and resources, and identity-bound submission.
- Import-graph architecture tests; private reconciliation and search services
  with consumer-defined dependencies and instance-owned background jobs.
- CI vulnerability scanning, full-suite coverage artifacts, short fuzz smoke,
  Dependabot updates, and test-gated main/release image publication.
- Tag releases with both CLIs for Linux/macOS/Windows on amd64/arm64, checksums,
  multi-platform images, SPDX source/container SBOMs and image attestations.
- Isolated `make test-integration`, `make vuln` and `make cover`; authenticated
  local transfer and agent-policy examples; accounting, verification, security,
  deployment and contribution guides.

### Changed

- Monetary posting arithmetic uses the same exact integer amount for source and
  destination; retained `rate` and `currency_multiplier` do not apply FX conversion.
- Inflight decimal conversion resolves stored precision through the injected
  datasource instead of global configuration/cache state, including bulk paths.
- Reconciliation orchestration moved into `internal/reconciliation`; reindex
  admission, progress and cancellation moved into an instance-owned search service.
- `LedgerForge.Close` cancels/waits for owned jobs and closes owned resources;
  callers retain ownership of injected datasource connections.
- Audit migrations and lifecycle immutability protection share a compatible
  transaction-update policy; sealed commitments and deletes remain protected.
- Audit/control migrations avoid transaction-table backfills, use a concurrent
  journal index and validate constraints separately; upgrade maintenance is
  documented for fresh installs and upgrades from main (`feda9b1`) or earlier
  published releases. This release introduces v3 directly; intermediate v1/v2
  migration sets were unreleased and are outside the supported upgrade paths.
  Retained v1/v2 decoders verify explicitly supplied older-format records only.
- Example configuration explicitly enables authentication. Compose requires a
  config file, binds published ports to loopback and starts workers after readiness.
- Go minimum is 1.26 with toolchain 1.26.9 selected. CI, Docker/release builds and
  the local compose test runner use Go 1.26.9.

### Fixed

- Rejected nonfinite/negative monetary controls, invalid integral precision,
  nonpositive precise amounts and positive amounts that round to zero units
  before execution; removed signed-int64 narrowing from large integer conversion.
- Deterministic decimal split allocation conserves units and rejects mixed or
  malformed distributions; leftover attribution no longer depends on float sums.
- Durable refund claims prevent concurrent duplicate reversals. Refunds preserve
  exact original units and apply only to `APPLIED` originals.
- Durable inflight remaining-capacity claims serialize partial commits, voids
  and accepted queued settlements; database updates fail atomically when invalid.
- Queue acceptance persists intent and action before dispatch, recovery preserves
  lifecycle meaning, and enqueue/settlement failure releases claims safely.
- Bulk saturation returns a controlled error without dereferencing a nil result.
- Bulk partial inflight commit reports missing/invalid items independently while
  valid items proceed in synchronous, queued and dry-run modes.
- Monetary JSON decoding preserves exact tokens across later `null` fields and
  rejects underflowing positive requests and lossy precision/overdraft controls.
- Replay compares intent and completion amounts and terminal cardinality,
  distinguishes grouping parents, and includes accepted pending commitments when
  validating remainder voids. New schema-marked ignored rates no longer cause
  false legacy failures.
- Audit upgrades retain historical rates and account for a recorded legacy
  column transition without rewriting INSERT-journal evidence or permitting
  later mutations.
- Legacy customer metadata with nonboolean flags or nonstring execution keys
  retains historical defaults during transaction reads and recovery. Raw INSERT
  evidence and canonical bytes remain unchanged; current controls stay strict.
- Notification/notifier setup and delivery use the originating instance's
  configuration, receiver and signing secret; borrowed datasource resources
  remain caller-owned.
- Outbox tests use isolated databases and verify cleanup, avoiding changes to
  pending lineage rows belonging to parallel package tests.
- Published main-branch tags include `:main`, matching compose and Makefile.

### Security

- Authentication preserves monetary JSON tokens while adding trusted metadata.
  Non-master key creation is owner-bound and cannot broaden scopes or expiry;
  listing/revocation derive ownership from the authenticated principal.
- Authentication checks authoritative PostgreSQL state rather than cached key
  validity; list responses hide stored hashes and successful legacy-key use can
  backfill its lookup prefix. Active-only legacy fallback is capped at two
  concurrent scans per process, returns 429 under saturation and can be disabled
  after rotation with `server.allow_legacy_api_keys`.
- API errors sanitize backend details across compatible response shapes; explicit
  body/upload bounds and route-resource mappings cover operational endpoints.
- Inflight commit/void preserves the exact safe `precise_amount must be positive`
  validation message; unstructured backend prefixes/suffixes remain sanitized.
- Delegation cannot remove inherited spending restrictions or multiply ancestor
  allowances. Descendants, siblings and intermediate delegated ancestors cannot
  manufacture approval authority within the initiator's family; explicit root
  or independent-family reviewers and master authority retain their defined roles.
- Public metadata updates reject reserved execution, lineage, policy and recovery
  keys; worker/recovery execution reads immutable controls instead of customer
  metadata.
- Dedicated webhook signing secrets with legacy fallback, protected HMAC headers,
  validated URLs, configurable private-destination controls, redirect rejection,
  delivery timeouts and bounded hook response bodies. Public-only mode rejects
  shared/internal special-purpose ranges at registration and connection time.
- Non-2xx notification/hook responses remain retryable instead of being acknowledged
  as successful delivery; signing uses the timestamp and exact raw JSON bytes.
- Built with Go 1.26.9 and `golang.org/x/net` 0.60.0. OpenTelemetry modules are
  aligned on 1.47, including the now-stable log API and SDK, with the Prometheus
  exporter at 0.69.0. Also updated: gin 1.12 (via otelgin 0.72), lib/pq 1.12.3,
  go-sqlite3 1.14.52 and ozzo-validation 4.4.1. `make vuln` reports no known
  vulnerabilities and remains a publication gate.

[Unreleased]: https://github.com/devaccuracy/ledgerforge/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/devaccuracy/ledgerforge/releases/tag/v1.0.0
