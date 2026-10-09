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
		for _, assignment := range []string{
			"amount = amount + 1", "precise_amount = precise_amount + 1", "precision = precision + 1",
			"rate = rate + 1", "parent_transaction = transaction_id", "source = destination",
			"effective_date = COALESCE(effective_date, created_at) + interval '1 day'",
			"id = id + 1000000", "transaction_id = transaction_id || '_changed'",
		} {
			_, err := ds.Conn.ExecContext(ctx, `UPDATE ledgerforge.transactions SET `+assignment+` WHERE transaction_id = $1`, txn.TransactionID)
			require.ErrorContains(t, err, "Cannot modify transaction data", assignment)
		}
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
	for _, assignment := range []string{"overdraft_limit = overdraft_limit + 1", "queue_action = 'COMMIT'"} {
		_, err := ds.Conn.ExecContext(ctx, `UPDATE ledgerforge.transactions SET `+assignment+` WHERE transaction_id = $1`, txn.TransactionID)
		require.ErrorContains(t, err, "Cannot modify transaction data", assignment)
	}
	count, err := ds.ChainPendingTransactions(ctx, time.Now().Add(time.Hour), 100)
	require.NoError(t, err)
	require.Equal(t, 1, count, "an unsealed row must accept all chain bookkeeping, including chain_version")
	assertSealed()

	// Roll back through the lifecycle migration by ID, including any successors.
	// The rate and chain policy belong to an earlier migration and must survive.
	const lifecycleID = "1791500001.sql"
	const lifecycleVersion int64 = 1791500001
	migrations := migrate.EmbedFileSystemMigrationSource{FileSystem: SQLFiles, Root: "sql"}
	migrationSet := migrate.MigrationSet{SchemaName: "ledgerforge"}
	plan, _, err := migrationSet.PlanMigrationToVersion(ds.Conn, "postgres", migrations, migrate.Down, lifecycleVersion)
	require.NoError(t, err)
	require.NotEmpty(t, plan)
	require.Equal(t, lifecycleID, plan[len(plan)-1].Id)
	count, err = migrationSet.ExecVersion(ds.Conn, "postgres", migrations, migrate.Down, lifecycleVersion)
	require.NoError(t, err)
	require.Equal(t, len(plan), count)
	var lifecycleApplied, claimsExist bool
	require.NoError(t, ds.Conn.QueryRow(`SELECT EXISTS (SELECT 1 FROM ledgerforge.gorp_migrations WHERE id = $1),
        to_regclass('ledgerforge.refund_claims') IS NOT NULL`, lifecycleID).Scan(&lifecycleApplied, &claimsExist))
	require.False(t, lifecycleApplied, "the lifecycle migration itself must have been rolled back")
	require.False(t, claimsExist, "rolling back a successor alone does not exercise lifecycle Down")
	assertPolicy()
	assertSealed()
	count, err = migrationSet.ExecVersion(ds.Conn, "postgres", migrations, migrate.Up, lifecycleVersion)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	assertPolicy()
	assertSealed()
	count, err = migrationSet.Exec(ds.Conn, "postgres", migrations, migrate.Up)
	require.NoError(t, err)
	require.Equal(t, len(plan)-1, count, "reapply exactly the rolled-back successors")
	count, err = migrationSet.Exec(ds.Conn, "postgres", migrations, migrate.Up)
	require.NoError(t, err)
	require.Zero(t, count, "migrate up remains idempotent")
	result, err := ds.VerifyChainSnapshot(ctx, nil)
	require.NoError(t, err)
	require.True(t, result.Verified, "%+v", result.Tampering)
}
