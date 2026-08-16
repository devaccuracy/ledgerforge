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
GO_FILES := $(shell find . -type f -name '*.go' -not -path './vendor/*')

.PHONY: \
	print-project init generate fmt fmt-check tidy-check vet test test-integration \
	build build-mcp verify docker-run run run-workers build-run build-test-run \
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

test-integration:
	go test ./...

build:
	go build -o $(PROJECT) ./cmd/ledgerforge

build-mcp:
	go build -o $(PROJECT)-mcp ./cmd/ledgerforge-mcp

verify: tidy-check fmt-check vet test build build-mcp

docker-run:
	docker run -v $(CURDIR)/ledgerforge.json:/ledgerforge.json -p 5001:5001 ghcr.io/devaccuracy/ledgerforge:main

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
