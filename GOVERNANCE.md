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

# Governance

LedgerForge is a maintainer-led open-source project. This document describes
how decisions are made today; it will change as the contributor base grows.

## Roles

- **Contributors** are anyone who opens an issue, reviews a change or submits
  a pull request. Follow the [Code of Conduct](CODE_OF_CONDUCT.md) and the
  [contributing guide](CONTRIBUTING.md).
- **Maintainers** review and merge pull requests, triage issues, cut releases,
  handle security reports and set the [roadmap](ROADMAP.md). Maintainers are
  listed in [CODEOWNERS](.github/CODEOWNERS), which also requests their review
  on every pull request.

## Decisions

- Most decisions happen in issues and pull requests. Maintainers aim for
  consensus with the people involved and decide when consensus is not reached.
- Changes to monetary behavior, the database schema, the public Go API, HTTP
  contracts or security boundaries need maintainer approval and the evidence
  described in [CONTRIBUTING.md](CONTRIBUTING.md).
- Larger changes start as a feature request so the design can be discussed
  before implementation.
- Releases follow semantic versioning and the
  [release checklist](docs/releasing.md); notable changes are recorded in the
  [changelog](CHANGELOG.md).
- Security issues are handled privately under the
  [security policy](SECURITY.md).

## Becoming a maintainer

Contributors who make sustained, high-quality contributions and reviews may be
invited to become maintainers by the existing maintainers. New maintainers are
added to CODEOWNERS in a pull request.

## Open source and commercial offerings

Everything in this repository is licensed under [Apache 2.0](LICENSE.md).
LedgerForge Cloud and Enterprise are services that operate this software;
offering them does not change the license of code in this repository.
Contributions are accepted under Apache 2.0 as described in
[CONTRIBUTING.md](CONTRIBUTING.md).
