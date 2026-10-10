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

# Roadmap

What has shipped and where LedgerForge is heading. Everything under **Next**
and **Later** is a plan, not a commitment: scope and order change with user
feedback, and there are no dates. Released changes are recorded in the
[changelog](CHANGELOG.md). To propose or discuss an item, open a
[feature request](https://github.com/devaccuracy/ledgerforge/issues/new/choose).

Everything in this repository is free under [Apache 2.0](LICENSE.md);
LedgerForge Cloud is the managed service built on it. See
[Open source and LedgerForge Cloud](README.md#open-source-and-ledgerforge-cloud).

## Shipped in 1.0

- **The exact double-entry core:** integer postings and balances, explicit
  precision, deterministic splits, immutable history and idempotent
  references, with inflight holds, refunds and queued execution.
- **Verification and the audit chain:** read-only `ledgerforge verify` replay,
  versioned hash chains with checkpoints, `audit verify` and `audit export`
  for anchoring roots outside the database.
- **Agent spending policies and approvals:** per-key limits, allowlists and
  daily budgets, shared budgets for delegated keys, and approval holds released
  by independent approvers.
- **The MCP server:** a local MCP server, read-only by default with opt-in
  write tools, that can be bound to an API key so its spending policy applies.
- **The hardened API:** owner-bound and constrained key delegation,
  revocation, bounded input, sanitized errors and signed webhooks with
  destination controls.
- **Release engineering:** versioned binaries for Linux, macOS and Windows,
  multi-platform images, checksums, SPDX SBOMs and image attestations, gated by
  test, vulnerability and fuzz checks.

Managed deployments are available by request through
[LedgerForge Cloud](https://ledgerforge.io/cloud).

## Next

Planned next, in no particular order.

- **LedgerForge Cloud multi-tenant service with a dashboard (private beta).**
- **Official client SDKs** for the HTTP API in TypeScript, Python and Go.
- **More MCP tools and agent-framework integrations.**
- **Crash and restart resilience testing.**

## Later

Planned further out, in no particular order.

- **SSO/SAML and role-based access for Cloud.**
- **Multi-currency policy budgets.**
- **Managed audit-checkpoint anchoring.**
- **Regional data residency options for Cloud.**
