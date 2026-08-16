# LedgerForge MCP server

`ledgerforge-mcp` exposes a configured LedgerForge deployment as a local Model
Context Protocol (MCP) server over standard input and output. It uses the same
LedgerForge configuration and database connection as the main service.

## Build and run

```bash
go build -o ledgerforge-mcp ./cmd/ledgerforge-mcp
./ledgerforge-mcp --config ./ledgerforge.json
```

Standard output is reserved for MCP protocol messages. Logs and audit events
are written to standard error.

## Client configuration

For a local MCP client that uses TOML configuration:

```toml
[mcp_servers.ledgerforge]
command = "/absolute/path/to/ledgerforge-mcp"
args = ["--config", "/absolute/path/to/ledgerforge.json"]
```

The server does not expose an HTTP endpoint. Run it as a local subprocess or
behind a separately authenticated MCP transport; never expose its stdio stream
through an unauthenticated proxy.

## Financial workflows

By default, the server is read-only and exposes tools to retrieve and list
ledgers, balances, transactions, historical balance states, and fund-lineage
data. Pagination is capped at 100 records per request.

Use the higher-level tools for normal financial work:

| Need | MCP capability |
| --- | --- |
| Find an account or wallet | `ledgerforge_find_balance` by indicator and currency |
| Investigate a payment | `ledgerforge_get_transaction_context`, which includes source and destination balance summaries |
| Audit a past position | `ledgerforge_get_balance_at_time` using snapshots or source transactions |
| Use LedgerForge data as context | `ledgerforge://ledgers/{ledger_id}`, `ledgerforge://balances/{balance_id}`, and `ledgerforge://transactions/{transaction_id}` resources |
| Guide an agent safely | `ledgerforge_investigate_balance`, `ledgerforge_investigate_transaction`, and `ledgerforge_prepare_transfer` prompts |

Every balance and transaction result includes an exact `*_minor` summary. These
values are base-10 integer strings and are the source of truth for accounting
decisions. Divide by `currency_multiplier` only to present an amount to a
human; never make accounting decisions from a floating-point amount.

State-changing tools are intentionally absent until the operator opts in:

```bash
./ledgerforge-mcp --config ./ledgerforge.json --allow-write
```

With `--allow-write`, the server additionally enables tools to create ledgers
and balances, transfer funds, create controlled bulk transfer batches, refund
eligible transactions, use advanced scheduled or split transaction workflows,
and commit or void inflight transactions. Each write tool invocation produces a
structured audit log entry.

Every write requires `confirm: true`. This is an explicit intent check, not an
authorization system: the MCP client configuration and the operating system
permissions around it remain the access-control boundary. Set `confirm: true`
only after an authorized human has approved the final action.

For ordinary transfers, use `ledgerforge_transfer_funds` rather than the
advanced queue tool. It accepts an exact integer amount and an idempotency
reference:

```json
{
  "source_balance_id": "bal_customer_cash",
  "destination_balance_id": "bal_merchant_payable",
  "amount_minor": "1234",
  "currency": "USD",
  "currency_multiplier": 100,
  "reference": "payment-order-1042",
  "description": "Order 1042",
  "confirm": true
}
```

This transfers exactly 1,234 minor units ($12.34 at a multiplier of 100).
Transaction references remain the idempotency key. `ledgerforge_create_bulk_transfers`
supports one to 100 transfers and can be made atomic; use `run_async` only when
your operational workflow can observe completion through its existing queue and
webhook operations.

## Production guidance

- Use a dedicated database credential with only the permissions required by
  the tools you choose to enable.
- Keep write access disabled for analysis, support, and reporting agents.
- Treat enabling `--allow-write` as production access: restrict who can change
  the MCP client configuration and collect standard-error audit logs.
- Keep the MCP server on local stdio or place a separately authenticated,
  authorized transport in front of it. `confirm: true` prevents accidental
  tool calls but does not replace identity, authorization, or approval policy.
- Run the existing LedgerForge workers for queued and scheduled transactions;
  the MCP server is an integration interface, not a replacement worker.
