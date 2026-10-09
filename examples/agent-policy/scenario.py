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

"""Executable HTTP assertions for spending policies and delegation families."""
import argparse
from datetime import datetime, timedelta, timezone
import json
import os
from pathlib import Path
import urllib.error
import urllib.request
import uuid


parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("config", type=Path)
parser.add_argument("--delegation-probes", action="store_true", help="require inherited policies, shared ancestor budgets and descendant approval denial")
args = parser.parse_args()
master = json.loads(args.config.read_text())["server"]["secret_key"]
url = "http://127.0.0.1:" + os.environ.get("LEDGERFORGE_API_PORT", "5001")
owner = "agent-example-" + uuid.uuid4().hex
expiry = (datetime.now(timezone.utc) + timedelta(hours=1)).isoformat().replace("+00:00", "Z")
created_keys = []


def request(key, method, path, payload=None):
    body = None if payload is None else json.dumps(payload).encode()
    req = urllib.request.Request(url + path, data=body, method=method, headers={
        "Content-Type": "application/json", "X-LedgerForge-Key": key,
    })
    try:
        with urllib.request.urlopen(req, timeout=30) as response:
            return response.status, json.load(response) if response.status != 204 else None
    except urllib.error.HTTPError as error:
        return error.code, json.load(error)


def success(key, method, path, payload=None):
    status, result = request(key, method, path, payload)
    if not 200 <= status < 300:
        raise RuntimeError(f"{method} {path} failed ({status}): {result}")
    return result


def create_key(name, scopes, creator=master):
    result = success(creator, "POST", "/api-keys", {
        "name": name, "owner": owner, "scopes": scopes, "expires_at": expiry,
    })
    created_keys.append(result["api_key_id"])
    return result


def balance(balance_id):
    return success(master, "GET", "/balances/" + balance_id)


def transfer_payload(amount, **extra):
    return dict({
        "source": source, "destination": destination, "currency": "USD",
        "precise_amount": amount, "precision": 100,
        "reference": "policy-example-" + uuid.uuid4().hex,
        "description": "Local policy example", "skip_queue": True,
    }, **extra)


def must_deny(key, method, path, payload=None):
    before = [balance(source), balance(destination)]
    status, result = request(key, method, path, payload)
    expected = (403,) if path.startswith("/approvals/") else (400, 403)
    if status not in expected:
        raise RuntimeError(f"Expected policy denial, got {status}: {result}")
    for old, current in zip(before, [balance(source), balance(destination)]):
        for field in ["balance", "credit_balance", "debit_balance", "inflight_credit_balance", "inflight_debit_balance"]:
            if old[field] != current[field]:
                raise RuntimeError("Denied operation changed " + field)


def assert_balances(spent, held=0):
    first, second = balance(source), balance(destination)
    if (first["balance"], second["balance"], first["inflight_debit_balance"], second["inflight_credit_balance"]) != (20000 - spent, spent, held, held):
        raise RuntimeError("Unexpected settled balances or reservation counters")
    all_balances = [balance(funding), first, second]
    if sum(b["credit_balance"] for b in all_balances) != sum(b["debit_balance"] for b in all_balances):
        raise RuntimeError("Double-entry conservation failed")


try:
    ledger = success(master, "POST", "/ledgers", {"name": owner})
    funding, source, destination = [success(master, "POST", "/balances", {
        "ledger_id": ledger["ledger_id"], "currency": "USD", "precision": 100,
    })["balance_id"] for _ in range(3)]
    success(master, "POST", "/transactions", dict(transfer_payload(20000), source=funding,
        destination=source, allow_overdraft=True, overdraft_limit=200))
    # Deliberately grant approval/delegation scopes to probe independence, not just missing permission.
    scopes = ["transactions:write", "transactions:read", "balances:read", "api-keys:write", "approvals:write"]
    agent = create_key("limited-agent", scopes)
    approver = create_key("independent-reviewer", ["approvals:write"])
    document = {
        "rules": [{"currency": "USD", "precision": 100, "max_transaction": "5000",
                   "daily_limit": "10000", "approval_above": "2000"}],
        "ledger_ids": [ledger["ledger_id"]], "balance_ids": [source, destination],
    }
    success(master, "PUT", "/policies/" + agent["api_key_id"], document)
    under = success(agent["key"], "POST", "/transactions", transfer_payload(1000))
    if under["status"] != "APPLIED":
        raise RuntimeError("Under-threshold transfer did not apply")
    assert_balances(1000)
    must_deny(agent["key"], "POST", "/transactions", transfer_payload(5001))
    held = success(agent["key"], "POST", "/transactions", transfer_payload(3000))
    if held["status"] != "INFLIGHT":
        raise RuntimeError("Above-threshold transfer did not create a hold")
    admission = held["meta_data"]["policy_admission_id"]
    assert_balances(1000, held=3000)
    must_deny(agent["key"], "POST", "/approvals/" + admission + "/approve")
    decision = success(approver["key"], "POST", "/approvals/" + admission + "/approve")
    if decision["decision"] != "approved":
        raise RuntimeError("Independent approval did not become final")
    assert_balances(4000)
    success(approver["key"], "POST", "/approvals/" + admission + "/approve")
    assert_balances(4000)
    capacity = success(agent["key"], "GET", "/policies/effective")["capacity"][0]
    if (capacity["used_minor"], capacity["remaining_minor"]) != ("4000", "6000"):
        raise RuntimeError("Admission was charged twice or capacity is inconsistent")
    print("PASS: limited transfer; maximum denial; threshold hold; denied self-approval")
    print("PASS: independent approval; decision retry; settled balances and daily capacity")

    if args.delegation_probes:
        child = create_key("delegated-agent", scopes, creator=agent["key"])
        grandchild = create_key("delegated-descendant", scopes, creator=child["key"])
        # A child without a local policy must still inherit its parent's cap.
        must_deny(child["key"], "POST", "/transactions", transfer_payload(5001))
        success(child["key"], "POST", "/transactions", transfer_payload(1500))
        success(agent["key"], "POST", "/transactions", transfer_payload(1500))
        assert_balances(7000)
        # Per-operation cap allows 4000, but shared ancestor usage would become 11000.
        must_deny(grandchild["key"], "POST", "/transactions", transfer_payload(4000))
        held = success(agent["key"], "POST", "/transactions", transfer_payload(2500))
        admission = held["meta_data"]["policy_admission_id"]
        assert_balances(7000, held=2500)
        for descendant in [child, grandchild]:
            for action in ["approve", "reject"]:
                must_deny(descendant["key"], "POST", "/approvals/" + admission + "/" + action)
        success(approver["key"], "POST", "/approvals/" + admission + "/approve")
        assert_balances(9500)
        print("PASS: inherited cap; shared ancestor budget; child and descendant approval denied")

        # Tighten the root threshold so a small descendant request needs review.
        document["rules"][0]["approval_above"] = "0"
        success(master, "PUT", "/policies/" + agent["api_key_id"], document)
        sibling = create_key("delegated-sibling", scopes, creator=agent["key"])
        held = success(child["key"], "POST", "/transactions", transfer_payload(100))
        if held["status"] != "INFLIGHT":
            raise RuntimeError("Inherited zero threshold did not create a hold")
        admission = held["meta_data"]["policy_admission_id"]
        assert_balances(9500, held=100)
        for actor in [child, grandchild, sibling]:
            for action in ["approve", "reject"]:
                must_deny(actor["key"], "POST", "/approvals/" + admission + "/" + action)
        success(agent["key"], "POST", "/approvals/" + admission + "/approve")
        assert_balances(9600)
        print("PASS: sibling/descendant decisions denied; explicit non-delegated root approved its child's hold")
    else:
        print("Run with --delegation-probes to also check inherited budgets and approval-family boundaries.")
finally:
    # Revoke every example credential even when a probe fails; never print their secrets.
    for key_id in reversed(created_keys):
        status, _ = request(master, "DELETE", "/api-keys/" + key_id + "?owner=" + owner)
        if status != 204:
            print("Could not revoke example key:", key_id)
