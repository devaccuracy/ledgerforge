/*
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
*/

package core

import (
	"context"
	"testing"
	"time"

	migrate "github.com/rubenv/sql-migrate"
	"github.com/stretchr/testify/require"
)

func TestAuditUnifiedImmutabilityPolicy(t *testing.T) {
	b, ds := isolatedIntegrityEngine(t)
	src, dst := newBalancePair(t, ds)
	txn := recordAppliedTxn(t, b, src.BalanceID, dst.BalanceID, 1)
	ctx := context.Background()
	assertPolicy := func() {
		t.Helper()
		var policies, separateChainPolicies, rateColumns int
		require.NoError(t, ds.Conn.QueryRowContext(ctx, `SELECT count(*) FILTER (WHERE tgname = 'prevent_transaction_update_trigger'),
   count(*) FILTER (WHERE tgname = 'prevent_sealed_chain_update_trigger')
   FROM pg_trigger WHERE tgrelid = 'ledgerforge.transactions'::regclass AND NOT tgisinternal`).Scan(&policies, &separateChainPolicies))
		require.Equal(t, 1, policies)
		require.Zero(t, separateChainPolicies)
		require.NoError(t, ds.Conn.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.columns
   WHERE table_schema = 'ledgerforge' AND table_name = 'transactions' AND column_name = 'rate'`).Scan(&rateColumns))
		require.Equal(t, 1, rateColumns)
	}
	assertDeleteBlocked := func() {
		t.Helper()
		_, err := ds.Conn.ExecContext(ctx, `DELETE FROM ledgerforge.transactions WHERE transaction_id = $1`, txn.TransactionID)
		require.ErrorContains(t, err, "Cannot delete transaction data")
	}
	assertSealed := func() {
		t.Helper()
		for _, assignment := range []string{
			"chain_seq = chain_seq + 1000000", "chain_prev_hash = repeat('f',64)",
			"chain_hash = repeat('f',64)", "chain_version = 1",
			"chain_seq = NULL, chain_prev_hash = NULL, chain_hash = NULL, chain_version = NULL",
		} {
			_, err := ds.Conn.ExecContext(ctx, `UPDATE ledgerforge.transactions SET `+assignment+` WHERE transaction_id = $1`, txn.TransactionID)
			require.ErrorContains(t, err, "Cannot modify a sealed transaction commitment", assignment)
		}
		_, err := ds.Conn.ExecContext(ctx, `UPDATE ledgerforge.transactions SET meta_data = '{"note":"allowed"}' WHERE transaction_id = $1`, txn.TransactionID)
		require.NoError(t, err)
		assertDeleteBlocked()
	}
	assertPolicy()
	assertDeleteBlocked()
	for _, assignment := range []string{"rate = rate + 1", "overdraft_limit = overdraft_limit + 1", "queue_action = 'COMMIT'"} {
		_, err := ds.Conn.ExecContext(ctx, `UPDATE ledgerforge.transactions SET `+assignment+` WHERE transaction_id = $1`, txn.TransactionID)
		require.ErrorContains(t, err, "Cannot modify transaction data", assignment)
	}
	count, err := ds.ChainPendingTransactions(ctx, time.Now().Add(time.Hour), 100)
	require.NoError(t, err)
	require.Equal(t, 1, count, "an unsealed row must accept all chain bookkeeping, including chain_version")
	assertSealed()

	// The lifecycle migration neither owns the rate column nor replaces the
	// chain policy, even when rolled back and reapplied independently.
	migrations := migrate.EmbedFileSystemMigrationSource{FileSystem: SQLFiles, Root: "sql"}
	count, err = migrate.ExecMax(ds.Conn, "postgres", migrations, migrate.Down, 1)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	assertPolicy()
	assertSealed()
	count, err = migrate.ExecMax(ds.Conn, "postgres", migrations, migrate.Up, 1)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	assertPolicy()
	assertSealed()
	count, err = migrate.Exec(ds.Conn, "postgres", migrations, migrate.Up)
	require.NoError(t, err)
	require.Zero(t, count, "migrate up remains idempotent")
	result, err := ds.VerifyChainSnapshot(ctx, nil)
	require.NoError(t, err)
	require.True(t, result.Verified, "%+v", result.Tampering)
}
