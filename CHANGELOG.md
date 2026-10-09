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

### Added

- `model.ParseAmount` and checked exact amount conversion, raw decimal-token
  preservation for HTTP transaction/inflight requests, and amount/distribution
  fuzz tests with independent arithmetic oracles.
- Read-only `ledgerforge verify` accounting replay with JSON issue reports and
  repeatable-read PostgreSQL snapshots of balances and lifecycle evidence.
- Canonical hash chain v2, versioned verification of existing v1 seals, persistent
  checkpoint roots, `ledgerforge audit verify`, `audit export`, external
  `--anchor` comparison, and separate pending-coverage/tampering reports.
- Generated PostgreSQL operation-sequence tests, concurrent snapshot/chainer
  tests, and property tests for mutation, deletion, truncation and retained roots.
- Per-key currency/precision spending rules, aggregate transaction caps, ledger
  and balance allowlists, UTC daily capacity, durable admissions and reservations.
- Policy approval holds, independent approval/rejection endpoints, effective
  policy/capacity inspection and persistence checks for queued execution.
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
- Notification/notifier setup and resource ownership follow the originating
  instance rather than closing borrowed datasource resources.
- Published main-branch tags include `:main`, matching compose and Makefile.

### Security

- Authentication preserves monetary JSON tokens while adding trusted metadata.
  Non-master key creation is owner-bound and cannot broaden scopes or expiry;
  listing/revocation derive ownership from the authenticated principal.
- Authentication checks authoritative PostgreSQL state rather than cached key
  validity; list responses hide stored hashes and successful legacy-key use can
  backfill its lookup prefix.
- API errors sanitize backend details across compatible response shapes; explicit
  body/upload bounds and route-resource mappings cover operational endpoints.
- Dedicated webhook signing secrets with legacy fallback, protected HMAC headers,
  validated URLs, configurable private-destination controls, redirect rejection,
  delivery timeouts and bounded hook response bodies.
- Non-2xx notification/hook responses remain retryable instead of being acknowledged
  as successful delivery; signing uses the timestamp and exact raw JSON bytes.
- Upgraded `golang.org/x/net` to 0.60.0, OTel logging API/SDK/stdout exporter to
  0.21.0, and aligned stable OTel APIs/SDK/OTLP exporters at 1.45.0 with Prometheus
  exporter 0.67.0. Go 1.26.9 and these modules remove the reviewed reachable
  vulnerability findings; `make vuln` remains a publication gate.
