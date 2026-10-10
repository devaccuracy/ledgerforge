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

## Problem and resulting behavior

Describe the concrete trigger and the behavior this change produces. Link the
related issue, for example `Closes #123`.

## Compatibility and operations

List API, configuration, schema or monetary behavior changes, migrations and
upgrade requirements, if any. Write "None" if there are none.

## Validation

Provide exact commands and results. Include focused financial tests and real
PostgreSQL concurrency tests when relevant, plus `make verify` and isolated
integration validation.

## Checklist

- [ ] `make verify` passes; `make test-integration` passes when persistence,
      workers or concurrency are affected.
- [ ] Financial changes have tests for exact amounts, balance conservation,
      idempotency and failed operations.
- [ ] Relevant docs, examples and the `[Unreleased]` section of `CHANGELOG.md`
      are updated.
- [ ] Existing license notices are preserved; new files carry the LedgerForge
      Apache 2.0 header.
- [ ] No secrets, API keys, or personal or financial data are included.
