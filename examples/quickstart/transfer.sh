#!/usr/bin/env bash
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

set -euo pipefail
example_dir=$(cd "$(dirname "$0")" && pwd)
python3 - "$example_dir/ledgerforge.local.json" <<'PYTHON'
import json
import os
import sys
import urllib.error
import urllib.request
import uuid

with open(sys.argv[1]) as file:
    key = json.load(file)["server"]["secret_key"]
url = "http://127.0.0.1:" + os.environ.get("LEDGERFORGE_API_PORT", "5001")


def request(method, path, payload=None):
    body = None if payload is None else json.dumps(payload).encode()
    req = urllib.request.Request(url + path, data=body, method=method, headers={
        "Content-Type": "application/json", "X-LedgerForge-Key": key,
    })
    try:
        with urllib.request.urlopen(req, timeout=30) as response:
            return response.status, json.load(response)
    except urllib.error.HTTPError as error:
        return error.code, json.load(error)


def success(method, path, payload=None):
    status, result = request(method, path, payload)
    if not 200 <= status < 300:
        sys.exit(f"{method} {path} failed ({status}): {result}")
    return result


# Each invocation creates a separate pair so the displayed invariant is self-contained.
ledger = success("POST", "/ledgers", {"name": "Quickstart " + uuid.uuid4().hex})
balances = [success("POST", "/balances", {
    "ledger_id": ledger["ledger_id"], "currency": "USD", "precision": 100,
}) for _ in range(2)]
source, destination = [balance["balance_id"] for balance in balances]
reference = "quickstart-" + uuid.uuid4().hex
payload = {
    "source": source, "destination": destination, "currency": "USD",
    "precise_amount": 1250, "precision": 100, "reference": reference,
    "description": "Local demo balanced transfer", "skip_queue": True,
    "allow_overdraft": True, "overdraft_limit": 12.50,
}
transaction = success("POST", "/transactions", payload)
if transaction["status"] != "APPLIED":
    sys.exit(f"Transfer did not settle: {transaction['status']}")


def verify():
    source_balance = success("GET", "/balances/" + source)
    destination_balance = success("GET", "/balances/" + destination)
    if [source_balance["balance"], destination_balance["balance"]] != [-1250, 1250]:
        sys.exit("Unexpected balances after posting or retry.")
    credits = sum(balance["credit_balance"] for balance in [source_balance, destination_balance])
    debits = sum(balance["debit_balance"] for balance in [source_balance, destination_balance])
    if credits != debits or credits != 1250:
        sys.exit("Double-entry conservation check failed.")


verify()
status, retry = request("POST", "/transactions", payload)
if status not in (400, 409):
    sys.exit(f"Duplicate reference was not rejected as expected ({status}): {retry}")
original = success("GET", "/transactions/reference/" + reference)
if original["transaction_id"] != transaction["transaction_id"]:
    sys.exit("Reference lookup did not recover the original posting.")
verify()
print("source: -1250 units")
print("destination: 1250 units")
print("total debits = total credits = 1250 units")
print("reference retry: balances unchanged")
print("transaction_id:", transaction["transaction_id"])
PYTHON
