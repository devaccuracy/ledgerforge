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

## Tool access

By default, the server is read-only and exposes tools to retrieve and list
ledgers, balances, transactions, and fund-lineage data. Pagination is capped at
100 records per request.

State-changing tools are intentionally absent until the operator opts in:

```bash
./ledgerforge-mcp --config ./ledgerforge.json --allow-write
```

With `--allow-write`, the server additionally enables tools to create ledgers
and balances, queue transactions, and commit or void inflight transactions.
Each write tool invocation produces a structured audit log entry. Transaction
references remain the idempotency key for transaction operations.

## Production guidance

- Use a dedicated database credential with only the permissions required by
  the tools you choose to enable.
- Keep write access disabled for analysis, support, and reporting agents.
- Treat enabling `--allow-write` as production access: restrict who can change
  the MCP client configuration and collect standard-error audit logs.
- Run the existing LedgerForge workers for queued and scheduled transactions;
  the MCP server is an integration interface, not a replacement worker.
