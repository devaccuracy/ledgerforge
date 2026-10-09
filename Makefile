# Copyright 2024 Blnk Finance Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

PROJECT ?= ledgerforge
GOVULNCHECK_VERSION ?= v1.1.4
TEST_ARGS ?= -count=1 ./...
COVER_DIR ?= /tmp/ledgerforge-coverage-$(shell id -u)
GO_FILES := $(shell find . -type f -name '*.go' -not -path './vendor/*')

.PHONY: \
	print-project init generate fmt fmt-check tidy-check vet test test-integration \
	build build-mcp verify readiness vuln cover docker-run run run-workers build-run build-test-run \
	migrate-up migrate-down backup backup-s3 \
	printProject docker_run run_workers build_run build_test_run migrate_up migrate_down backup_s3

print-project:
	echo $(PROJECT)

# Backward-compatible aliases for the historical underscore target names.
printProject: print-project

init:
	go mod download

generate:
	go generate ./...

fmt:
	gofmt -w $(GO_FILES)

fmt-check:
	test -z "$(shell gofmt -l $(GO_FILES))"

tidy-check:
	go mod tidy
	git diff --exit-code -- go.mod go.sum

vet:
	go vet ./...

test:
	go test -short ./...

# Run in a private compose network, including migrations; always clean up this run.
# Override TEST_ARGS to select packages or enable the race detector.
test-integration:
	@project="ledgerforge-test-$$(id -u)-$$$$"; \
	trap 'docker compose -p "$$project" -f docker-compose.test.yaml down -v' EXIT; \
	docker compose -p "$$project" -f docker-compose.test.yaml run --rm test $(TEST_ARGS)

vuln:
	@tool_dir=$$(mktemp -d); \
	trap 'rm -r "$$tool_dir"' EXIT; \
	GOBIN="$$tool_dir" go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) && \
	"$$tool_dir/govulncheck" ./...

cover:
	@mkdir -p "$(COVER_DIR)"
	go test -short -coverprofile="$(COVER_DIR)/coverage.out" -covermode=atomic ./...
	go tool cover -html="$(COVER_DIR)/coverage.out" -o "$(COVER_DIR)/index.html"

# Mutation testing on the money-critical fast packages (model, filter).
# Plants hundreds of one-line bugs and re-runs the tests against each one;
# a "LIVED" mutant is a bug the test suite would not catch. Fails when test
# efficacy (killed/viable mutants) drops below the threshold. Takes ~10 min.
# Triage survivors by reading the LIVED lines; model/mutation_killers_test.go
# has examples of boundary tests written to kill them.
# (The threshold is enforced here by parsing the score: gremlins' own
# --threshold-efficacy flag does not affect its exit code.)
MUTATION_THRESHOLD=80
mutate:
	@command -v gremlins >/dev/null 2>&1 || go install github.com/go-gremlins/gremlins/cmd/gremlins@latest
	@for pkg in model internal/filter; do \
		echo "==> mutation testing $$pkg (threshold ${MUTATION_THRESHOLD}%)"; \
		log=/tmp/gremlins-$$(basename $$pkg).log; \
		(cd $$pkg && gremlins unleash --workers 2 --timeout-coefficient 8) | tee $$log; \
		eff=$$(grep -o 'Test efficacy: [0-9.]*' $$log | grep -o '[0-9.]*'); \
		awk -v e="$$eff" -v t="${MUTATION_THRESHOLD}" 'BEGIN { exit (e+0 < t+0) ? 1 : 0 }' \
			|| { echo "MUTATION GATE FAILED: $$pkg efficacy $$eff% is below ${MUTATION_THRESHOLD}% — see LIVED lines in $$log"; exit 1; }; \
		echo "PASS: $$pkg efficacy $$eff%"; \
	done

build:
	go build -o $(PROJECT) ./cmd/ledgerforge

build-mcp:
	go build -o $(PROJECT)-mcp ./cmd/ledgerforge-mcp

readiness:
	python3 .github/scripts/test-readiness.py
	python3 -m unittest discover -s examples/webhook-verification -p 'test_*.py'

verify: tidy-check fmt-check readiness vet test build build-mcp

docker-run:
	docker run --mount type=bind,source="$(CURDIR)/ledgerforge.json",target=/ledgerforge.json,readonly -p 127.0.0.1:5001:5001 ghcr.io/devaccuracy/ledgerforge:main

docker_run: docker-run

run:
	./$(PROJECT) start

run-workers:
	./$(PROJECT) workers

run_workers: run-workers

build-run:
	make build
	make run

build_run: build-run

build-test-run:
	make build
	make test
	make run

build_test_run: build-test-run

migrate-up:
	./$(PROJECT) migrate up

migrate_up: migrate-up

migrate-down:
	./$(PROJECT) migrate down

migrate_down: migrate-down

backup:
	./$(PROJECT) backup drive

backup-s3:
	./$(PROJECT) backup s3

backup_s3: backup-s3
