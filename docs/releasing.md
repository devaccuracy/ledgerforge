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

# Release checklist

Use a reviewed commit on `main`. Complete this checklist before creating a
`vMAJOR.MINOR.PATCH` tag; use a suffix such as `-rc.1` for prereleases.

1. Consolidate the merged changes under `Unreleased` in [CHANGELOG.md](../CHANGELOG.md).
   Create the version/date entry and leave a fresh `Unreleased` section.
2. Check API and config compatibility, migrations, license notices, current
   security findings and dependency updates. Confirm that new financial behavior
   has focused tests and service-backed concurrency coverage where needed.
3. Run `make verify`, `make test-integration` and `make vuln` with an up-to-date Go
   patch version. Exercise the [quickstart](../examples/quickstart/README.md).
4. Review operational changes, backup/restore expectations and upgrade notes.
   Resolve the README's integration comments when the relevant features land.
5. Create and push the version tag through the maintainer's normal review process.
   The [release workflow](../.github/workflows/release.yml) retests that exact
   commit before building archives or publishing the container.
6. Inspect the generated GitHub release, download the assets and run
   `sha256sum -c checksums.txt` in the download directory. Review the generated
   notes and include migration and compatibility guidance.
7. Check the recorded container digest and the two SPDX SBOMs. Smoke-test the
   native CLI archives and the image, then announce the release.

Archives include `ledgerforge`, `ledgerforge-mcp`, `LICENSE.md` and `NOTICE.md`
for Linux, macOS and Windows on amd64 and arm64. The workflow builds without
CGO, so optional SQLite heartbeat storage is unavailable in these binaries.
Checksums cover the archives, source/container SBOMs and recorded image digest.
Container builds also request BuildKit SBOM/provenance attestations.

Main-branch publication creates `:main`, `:latest` and a commit tag only after
its test gate succeeds. Version releases publish version tags and leave those
main-branch aliases unchanged. Prereleases are marked as such on GitHub. Failed
vulnerability checks intentionally block publication until remediation.

The test gate is a local [reusable workflow](https://docs.github.com/en/actions/how-tos/reuse-automations/reuse-workflows),
so it uses the caller's commit without polling unrelated branch checks. The
workflow can resume publication by rerunning failed jobs for the same tag;
release asset uploads replace files with the same name.
