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

"""Check APPLIED refunds and reject VOID originals against the local demo."""
import json
import os
from pathlib import Path
import urllib.error
import urllib.request
import uuid


key = json.loads(Path(__file__).with_name("ledgerforge.local.json").read_text())["server"]["secret_key"]
url = "http://127.0.0.1:" + os.environ.get("LEDGERFORGE_API_PORT", "5001")


def request(method, path, payload=None):
    req = urllib.request.Request(url + path, data=None if payload is None else json.dumps(payload).encode(),
        method=method, headers={"Content-Type": "application/json", "X-LedgerForge-Key": key})
    try:
        with urllib.request.urlopen(req, timeout=30) as response:
            return response.status, json.load(response)
    except urllib.error.HTTPError as error:
        return error.code, json.load(error)


def success(method, path, payload=None):
    status, result = request(method, path, payload)
    if not 200 <= status < 300:
        raise RuntimeError(f"{method} {path} failed ({status}): {result}")
    return result


ledger = success("POST", "/ledgers", {"name": "Refund regression " + uuid.uuid4().hex})
source, destination = [success("POST", "/balances", {
    "ledger_id": ledger["ledger_id"], "currency": "USD", "precision": 100,
})["balance_id"] for _ in range(2)]


def transfer(inflight=False):
    return success("POST", "/transactions", {
        "source": source, "destination": destination, "currency": "USD", "precision": 100,
        "precise_amount": 100, "reference": "refund-contract-" + uuid.uuid4().hex,
        "description": "Local refund contract", "skip_queue": True,
        "inflight": inflight, "allow_overdraft": True, "overdraft_limit": 1,
    })


applied = transfer()
refund = success("POST", "/refund-transaction/" + applied["transaction_id"], {"skip_queue": True})
if refund["status"] != "APPLIED":
    raise RuntimeError("APPLIED original refund did not settle")
hold = transfer(inflight=True)
voided = success("PUT", "/transactions/inflight/" + hold["transaction_id"], {"status": "void", "skip_queue": True})
if voided["status"] != "VOID":
    raise RuntimeError("Hold was not voided")
before = [success("GET", "/balances/" + identifier) for identifier in [source, destination]]
status, result = request("POST", "/refund-transaction/" + voided["transaction_id"], {"skip_queue": True})
if status != 404 or result.get("error_detail", {}).get("code") != "TXN_NOT_FOUND":
    raise RuntimeError(f"VOID original refund was not rejected: {status}: {result}")
after = [success("GET", "/balances/" + identifier) for identifier in [source, destination]]
for old, current in zip(before, after):
    for field in ["balance", "credit_balance", "debit_balance", "inflight_credit_balance", "inflight_debit_balance"]:
        if old[field] != current[field]:
            raise RuntimeError("Rejected VOID refund changed " + field)
print("PASS: APPLIED refund; VOID original rejected without balance effects")
