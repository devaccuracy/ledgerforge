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

# Balanced transfer quickstart (local only)

This example builds the current checkout and runs an authenticated API with
PostgreSQL and Redis in a dedicated `ledgerforge-demo` compose project. The API
binds only to `127.0.0.1:5001`; the database and Redis publish no host ports.
The random master secret is stored in the ignored, mode-0600
`ledgerforge.local.json`. The database password is a **local-only demo value**.

Requires Docker with a Compose plugin supporting `up --wait`, and Python 3.
Run from the repository root:

```sh
./examples/quickstart/start.sh
./examples/quickstart/transfer.sh
```

If port 5001 is already in use, set `LEDGERFORGE_API_PORT` for both commands:

```sh
LEDGERFORGE_API_PORT=55001 ./examples/quickstart/start.sh
LEDGERFORGE_API_PORT=55001 ./examples/quickstart/transfer.sh
```

The transfer script creates a ledger and two fresh USD balances on every run,
posts synchronously, checks balance conservation, retries the same reference
and verifies the original transaction and unchanged balances. It uses Python's
integer-preserving JSON parser and needs no third-party client dependencies.

The request to `POST /transactions` is:

```json
{
  "source": "<created source balance_id>",
  "destination": "<created destination balance_id>",
  "currency": "USD",
  "precise_amount": 1250,
  "precision": 100,
  "reference": "<unique business operation reference>",
  "description": "Local demo balanced transfer",
  "skip_queue": true,
  "allow_overdraft": true,
  "overdraft_limit": 12.50
}
```

Requests carry `X-LedgerForge-Key`. The example reads its key from the local
config rather than printing it. A bounded overdraft allows the initially empty
source to post a debit position of `-12.50`; it does not represent real funding.
The destination receives `12.50`. Both counters and balance values use units:

```text
source: -1250 units
destination: 1250 units
total debits = total credits = 1250 units
reference retry: balances unchanged
```

Stop this stack without deleting its data:

```sh
docker compose -p ledgerforge-demo -f examples/quickstart/compose.yaml down
```

To discard only this demo's database, add `-v` to that command. Keep the local
config if restarting the same demo; deleting it generates a new key next time.
Use the [production deployment guide](../../docs/deployment.md) before exposing
any service beyond your machine.

Check the documented refund eligibility against the running API:

```sh
python3 examples/quickstart/refund-contract.py
```

This regression settles a refund of an APPLIED posting, then voids a reservation
and verifies that refunding the VOID row is rejected without counter changes.

Check the verification CLI without changing database evidence:

```sh
python3 examples/quickstart/audit-contract.py
```

With sealing off, it checks replay and the distinct pending-coverage result. To
exercise retained roots, add `"transaction": {"hash_chain": {"enabled": true}}`
to `ledgerforge.local.json`, restart the **server** service and allow at least
35 seconds for sealing before rerunning. The complete check exports roots,
verifies them, then supplies a conflicting root and requires an audit failure.
It does not alter the stored chain or supply external signature authentication.
See the [external anchoring runbook](../../docs/verification.md).
