# Contributing to LedgerForge

Keep changes focused, explain the behavior they address, and include evidence
that the ledger remains correct. Follow the [Code of Conduct](CODE_OF_CONDUCT.md).
Use GitHub issues for bugs and proposals; report vulnerabilities through the
[private security channel](SECURITY.md).

## Development setup

Install Go **1.26 or newer**, Git, Docker and the Docker Compose plugin. The
module declares Go 1.25 compatibility; CI and the integration runner use the
current Go 1.26 patch line. The quickstart also needs Python 3.

```sh
git clone https://github.com/devaccuracy/ledgerforge.git
cd ledgerforge
go mod download
make build build-mcp
make verify
```

`make verify` checks module consistency, Go formatting, vet, the short test suite
and both CLI builds. Short tests use mocks or in-memory services and need no
externally managed database. For a local API, use the
[authenticated quickstart](examples/quickstart/README.md).

## Isolated integration tests

```sh
make test-integration
# Select packages or enable races against real PostgreSQL:
make test-integration TEST_ARGS='-race -count=1 ./database ./internal/core'
```

This target runs `docker-compose.test.yaml` in a unique project. PostgreSQL 16,
Redis and Typesense share a private network namespace with the Go test container.
Tests' `localhost:5432`, `localhost:6379` and `localhost:8108` therefore refer to
that stack. No host ports are published and no existing containers are reused.
Migrations run first; the target tears down its containers and volumes on exit.
The test runner mounts your checkout, so it runs your current edits.

To inspect a failure before cleanup, run compose directly with a dedicated name:

```sh
docker compose -p ledgerforge-test-debug -f docker-compose.test.yaml run --rm test -count=1 ./...
docker compose -p ledgerforge-test-debug -f docker-compose.test.yaml logs
docker compose -p ledgerforge-test-debug -f docker-compose.test.yaml down -v
```

Avoid running full tests on the host against unrelated services. PostgreSQL and
Redis must be isolated; many integration tests use fixed localhost addresses.

## Additional checks

```sh
make vuln
make cover
.github/scripts/fuzz-smoke.sh
```

`make vuln` installs a pinned govulncheck in a temporary directory, scans
reachable vulnerabilities and fails on findings. Update dependencies or the Go
toolchain when appropriate; do not suppress a finding just to publish a release.
`make cover` writes a short-suite profile and HTML report under
`/tmp/ledgerforge-coverage-$(id -u)`; override `COVER_DIR` if needed. CI uploads a
full-suite coverage profile as an artifact. The fuzz smoke discovers available
fuzz targets and exercises each for five seconds, using short mode.

If Go cannot obtain VCS status because of your local Git environment, use
`GOFLAGS=-buildvcs=false` for validation. Do not change shared Git metadata to
work around that error.

## Code, commits and pull requests

Preserve existing license notices. New code uses the LedgerForge copyright
header and Apache 2.0 boilerplate. Follow Go conventions, format changed Go files,
and keep public API, JSON and configuration changes backward compatible.
Schema changes need an ordered migration in `internal/core/sql` and tests.

Financial behavior needs focused tests for exact amounts, balance conservation,
idempotency and failed operations. Concurrency changes need race tests against
real PostgreSQL where locking or transaction boundaries matter. Include rollback
and retry cases. Avoid unrelated refactoring and generated noise.

Use a branch from current `main` and logical commits with clear, imperative
messages. A PR should state the problem, resulting behavior, compatibility
impact and exact validation commands/results. Update relevant docs and examples.
The [PR template](.github/PULL_REQUEST_TEMPLATE.md) captures these expectations.
Maintainers may ask for a smaller scope or additional evidence before merging.

**No DCO sign-off is required.** By submitting a contribution you agree that it
is licensed under the repository's Apache 2.0 license. Keep attribution for any
third-party material you introduce.

## Releases

Maintainers use the [release checklist](docs/releasing.md). A `v*` tag invokes
all build, vet, race, integration, vulnerability and fuzz gates for that tag's
commit before packaging or publishing. The workflow creates both CLIs,
checksums, a multi-platform image and SPDX SBOMs.
