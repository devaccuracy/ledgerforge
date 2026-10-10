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

# LedgerForge documentation

Every guide in this repository, grouped by task. The same material is published
at [ledgerforge.io/docs](https://ledgerforge.io/docs). New to LedgerForge? Run
the [quickstart](../examples/quickstart/README.md), then read
[concepts](concepts.md).

## Understand the ledger

- [Concepts](concepts.md): ledgers, balances, precision and rounding,
  transaction states, inflight reservations, refunds and previews.
- [Architecture](../ARCHITECTURE.md): package layout, dependency rules and
  trust boundaries.

## Run it in production

- [Deployment](deployment.md): authenticated configuration, secrets, TLS,
  request limits, workers, releases and webhook verification.
- [Integrity upgrade notes](upgrade-notes.md): upgrade paths, migrations and
  maintenance for historical integrity evidence.
- [Metrics](metrics.md): the Prometheus-compatible metrics the server and
  workers expose, and what they tell you.
- [Metadata webhooks](metadata-webhooks.md): events and payloads sent when
  metadata changes.

## Prove and protect the ledger

- [Verification](verification.md): read-only accounting replay, versioned hash
  chains, checkpoints and external audit anchoring.
- [Security operations](security.md): scoped keys, delegation, revocation,
  rotation and input handling.

## Put AI agents on the ledger

- [Agent safety](agent-safety.md): per-key spending policies, shared budgets,
  independent approval holds and the threat model.
- [MCP server](mcp.md): run `ledgerforge-mcp`, configure clients, bind it to an
  API key and enable write tools deliberately.

## Examples

- [Quickstart](../examples/quickstart/README.md): a local, authenticated stack
  and a balanced transfer.
- [Agent policy](../examples/agent-policy/README.md): an executable scenario
  for spending limits and approval.
- [Webhook verification](../examples/webhook-verification/README.md): verify
  signed webhook deliveries.
- [Load testing](../tests/loadtest/README.md): measure API acceptance and queue
  drain time.

## Project

- [Contributing](../CONTRIBUTING.md): development setup and isolated
  integration tests.
- [Release checklist](releasing.md): how maintainers cut and verify a release.
- [Roadmap](../ROADMAP.md), [changelog](../CHANGELOG.md),
  [security policy](../SECURITY.md), [support](../SUPPORT.md) and
  [governance](../GOVERNANCE.md).
