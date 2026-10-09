#!/usr/bin/env python3
# Copyright 2026 LedgerForge Contributors.
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

"""Release contract regressions: dependency floors, patched build paths and docs."""
import json
from pathlib import Path
import re
import subprocess
import unittest


ROOT = Path(__file__).resolve().parents[2]


def read(name):
    return (ROOT / name).read_text()


def version(value):
    match = re.search(r"(?:go|v)?(\d+)\.(\d+)\.(\d+)", value)
    if not match:
        raise ValueError("not a numeric release version: " + value)
    return tuple(map(int, match.groups()))


class ReleaseReadinessTests(unittest.TestCase):
    def test_vulnerable_module_versions_cannot_return(self):
        metadata = json.loads(subprocess.check_output(["go", "mod", "edit", "-json"], cwd=ROOT))
        modules = {entry["Path"]: entry["Version"] for entry in metadata["Require"]}
        self.assertGreaterEqual(version(modules["golang.org/x/net"]), (0, 60, 0))
        for module in ["sdk/log", "log", "exporters/stdout/stdoutlog"]:
            self.assertGreaterEqual(version(modules["go.opentelemetry.io/otel/" + module]), (0, 21, 0))
        stable = modules["go.opentelemetry.io/otel"]
        for suffix in ["metric", "trace", "sdk", "sdk/metric", "exporters/otlp/otlptrace", "exporters/otlp/otlptrace/otlptracehttp", "exporters/otlp/otlpmetric/otlpmetrichttp"]:
            self.assertEqual(modules["go.opentelemetry.io/otel/" + suffix], stable)
        self.assertGreaterEqual(version(metadata["Toolchain"]), (1, 26, 9))

    def test_all_build_paths_use_patched_toolchains(self):
        for file in [".github/workflows/go.yml", ".github/workflows/release.yml", ".github/workflows/lint.yml"]:
            versions = re.findall(r"go-version:\s*['\"]?([^'\"\s]+)", read(file))
            self.assertTrue(versions, file)
            for selected in versions:
                self.assertGreaterEqual(version(selected), (1, 26, 9), file)
        for file in ["Dockerfile", "docker-compose.test.yaml"]:
            images = re.findall(r"golang:(\d+\.\d+\.\d+)", read(file))
            self.assertTrue(images, file)
            for image in images:
                self.assertGreaterEqual(version(image), (1, 26, 9), file)

    def test_webhook_operations_document_current_contract(self):
        guide = read("docs/deployment.md")
        for requirement in ["notification.webhook.signing_secret", "LEDGERFORGE_WEBHOOK_SIGNING_SECRET", "notification.webhook.block_private_destinations", "X-LedgerForge-Timestamp", "X-LedgerForge-Signature", 'hex(HMAC-SHA256(secret, timestamp+"."+raw_body))', "constant time", "idempotency"]:
            self.assertIn(requirement, guide)
        self.assertRegex(guide, r"(?s)legacy fallback only when the dedicated value is empty")
        self.assertRegex(guide, r"(?s)headers are protected.*replaces configured")
        self.assertNotIn("current configuration can override them", guide)
        self.assertNotIn("receiver holding it has ledger", guide)

    def test_refund_eligibility_has_no_void_exception(self):
        refunds = read("docs/concepts.md").split("## Refunds and previews", 1)[1]
        self.assertRegex(refunds, r"Refunds apply only to `APPLIED`")
        self.assertRegex(refunds, r"(?s)`VOID`.*originals are not refundable")
        self.assertNotIn("accepts originals in `APPLIED` or `VOID`", refunds)

    def test_release_presentation_is_complete_and_links_resolve(self):
        files = [ROOT / name for name in ["README.md", "ARCHITECTURE.md", "CHANGELOG.md"]]
        files += list((ROOT / "docs").glob("*.md"))
        files += list((ROOT / "examples").rglob("*.md"))
        for file in files:
            content = file.read_text()
            self.assertNotRegex(content.lower(), r"integration placeholder|integration comments")
            for target in re.findall(r"\]\(([^)]+)\)", content):
                target = target.split("#", 1)[0]
                if target and not re.match(r"[a-z]+:", target):
                    self.assertTrue((file.parent / target).exists(), str(file) + ": " + target)
        self.assertIn("the API server owns the hash-chain processor", read("docs/verification.md"))
        for guide in ["docs/verification.md", "docs/agent-safety.md", "docs/security.md"]:
            self.assertIn(guide, read("README.md"))


if __name__ == "__main__":
    unittest.main()
