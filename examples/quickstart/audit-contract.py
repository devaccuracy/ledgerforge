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

"""Read-only CLI regressions against the local demo; no database mutations."""
import copy
import json
from pathlib import Path
import subprocess


compose = ["docker", "compose", "-p", "ledgerforge-demo", "-f",
           str(Path(__file__).with_name("compose.yaml")), "exec", "-T", "server", "ledgerforge"]


def command(*args, stdin=None):
    result = subprocess.run(compose + list(args), input=stdin, capture_output=True, text=True)
    try:
        report = json.loads(result.stdout)
    except json.JSONDecodeError as error:
        raise RuntimeError("CLI did not return JSON: " + result.stderr) from error
    return result, report


result, replay = command("verify", "--json")
if result.returncode or not replay["verified"] or replay["issues"]:
    raise RuntimeError("Accounting replay failed: " + json.dumps(replay))
result, audit = command("audit", "verify", "--json")
if not audit["verified"] or audit["tampering"]:
    raise RuntimeError("Audit evidence conflicts: " + json.dumps(audit))
if audit["pending"]:
    if not result.returncode or "coverage pending" not in result.stderr:
        raise RuntimeError("Pending coverage did not have its distinct CLI result")
    print("PASS: replay and intact chain; CLI distinguishes pending coverage from tampering")
    print("Enable transaction.hash_chain.enabled in the local config, restart server, wait for sealing and rerun.")
else:
    if result.returncode:
        raise RuntimeError("Fully covered audit unexpectedly failed: " + result.stderr)
    result, anchors = command("audit", "export")
    if result.returncode or not anchors:
        raise RuntimeError("The local demo needs sealed history before testing retained roots")
    result, retained = command("audit", "verify", "--anchor", "/dev/stdin", "--json",
                               stdin=json.dumps(anchors))
    if result.returncode or not retained["verified"] or retained["tampering"]:
        raise RuntimeError("Exported roots did not verify")
    changed = copy.deepcopy(anchors)
    root = changed[0]["head_hash"]
    changed[0]["head_hash"] = ("1" if root[0] == "0" else "0") + root[1:]
    result, mismatch = command("audit", "verify", "--anchor", "/dev/stdin", "--json",
                               stdin=json.dumps(changed))
    if not result.returncode or mismatch["verified"] or not mismatch["tampering"]:
        raise RuntimeError("Conflicting retained root was not detected")
    print("PASS: accounting replay, complete audit, exported roots and conflicting external anchor")
