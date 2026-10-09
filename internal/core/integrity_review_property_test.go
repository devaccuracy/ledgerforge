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
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/devaccuracy/ledgerforge/database"
	"github.com/devaccuracy/ledgerforge/internal/integrity"
	"github.com/devaccuracy/ledgerforge/model"
	migrate "github.com/rubenv/sql-migrate"
	"github.com/stretchr/testify/require"
)

func TestIntegrityIntentCompletionAmountAndCardinality(t *testing.T) {
	for _, scenario := range []string{"precise amount", "major amount", "duplicate completion", "wrong terminal status"} {
		t.Run(scenario, func(t *testing.T) {
			b, ds := isolatedIntegrityEngine(t)
			src, dst := newBalancePair(t, ds)
			intent := &model.Transaction{TransactionID: model.GenerateUUIDWithSuffix("txn"), Reference: model.GenerateUUIDWithSuffix("ref"), Source: src.BalanceID, Destination: dst.BalanceID, PreciseAmount: big.NewInt(100), Precision: 1, Currency: "USD", Status: StatusQueued, AllowOverdraft: true}
			setTransactionMetadata(intent)
			_, err := ds.RecordTransaction(t.Context(), intent)
			require.NoError(t, err)
			child := createQueueCopy(intent, intent.Reference)
			child.Status = StatusApplied
			if scenario == "wrong terminal status" {
				child.Status = StatusInflight
				child.Inflight = true
			}
			if scenario == "precise amount" {
				child.PreciseAmount = big.NewInt(200)
				model.ApplyPrecision(child)
			}
			_, err = b.RecordTransaction(t.Context(), child)
			require.NoError(t, err)
			if scenario == "duplicate completion" {
				child.TransactionID = model.GenerateUUIDWithSuffix("txn")
				child.Reference = model.GenerateUUIDWithSuffix("ref")
				_, err = b.RecordTransaction(t.Context(), child)
				require.NoError(t, err)
			}
			if scenario == "major amount" {
				tx, err := ds.Conn.Begin()
				require.NoError(t, err)
				_, err = tx.Exec(`SET LOCAL session_replication_role=replica`)
				require.NoError(t, err)
				_, err = tx.Exec(`UPDATE ledgerforge.transactions SET amount=200 WHERE transaction_id=$1`, child.TransactionID)
				require.NoError(t, err)
				require.NoError(t, tx.Commit())
			}
			report, err := integrity.Verify(t.Context(), ds)
			require.NoError(t, err)
			require.False(t, report.Verified, "%+v", report)
			found := false
			for _, issue := range report.Issues {
				if issue.Kind == "inconsistent_child" {
					found = true
				}
			}
			require.True(t, found, "%+v", report.Issues)
		})
	}
}

func TestIntegrityCurrentEconomicSchemaAndGrouping(t *testing.T) {
	for _, scenario := range []string{"split", "bulk", "ignored rate", "pending commit and void"} {
		t.Run(scenario, func(t *testing.T) {
			b, ds := isolatedIntegrityEngine(t)
			src, dst := newBalancePair(t, ds)
			draft := func() *model.Transaction {
				return &model.Transaction{TransactionID: model.GenerateUUIDWithSuffix("txn"), Reference: model.GenerateUUIDWithSuffix("ref"), Source: src.BalanceID, Destination: dst.BalanceID, Amount: 100, Precision: 1, Rate: 1.25, Currency: "USD", AllowOverdraft: true, SkipQueue: true}
			}
			switch scenario {
			case "split":
				second, err := ds.CreateBalance(model.Balance{Currency: "USD", LedgerID: "general_ledger_id"})
				require.NoError(t, err)
				txn := draft()
				txn.Destination = ""
				txn.Destinations = []model.Distribution{{Identifier: dst.BalanceID, Distribution: "40"}, {Identifier: second.BalanceID, Distribution: "left"}}
				_, err = b.QueueTransaction(t.Context(), txn)
				require.NoError(t, err)
			case "bulk":
				_, err := b.CreateBulkTransactions(t.Context(), &model.BulkTransactionRequest{Transactions: []*model.Transaction{draft(), draft()}, SkipQueue: true})
				require.NoError(t, err)
			case "ignored rate":
				_, err := b.QueueTransaction(t.Context(), draft())
				require.NoError(t, err)
			case "pending commit and void":
				hold := lifecycleHold(t, b, ds)
				intent := lifecycleIntent(hold, 4000)
				_, err := ds.RecordTransaction(t.Context(), intent)
				require.NoError(t, err)
				_, err = b.VoidInflightTransaction(t.Context(), hold.TransactionID)
				require.NoError(t, err)
			}
			if scenario == "split" || scenario == "bulk" {
				snapshot, err := ds.ReadIntegritySnapshot(t.Context())
				require.NoError(t, err)
				for _, row := range snapshot.Transactions {
					if row.Status == StatusApplied {
						_, err = b.RefundTransaction(t.Context(), row.TransactionID, true)
						require.NoError(t, err)
						break
					}
				}
			}
			report, err := integrity.Verify(t.Context(), ds)
			require.NoError(t, err)
			require.True(t, report.Verified, "%+v", report.Issues)
		})
	}
}

func TestAuditExecutionControlsCannotBeChangedThroughMetadata(t *testing.T) {
	b, ds := isolatedIntegrityEngine(t)
	src, dst := newBalancePair(t, ds)
	intent := &model.Transaction{TransactionID: model.GenerateUUIDWithSuffix("txn"), Reference: model.GenerateUUIDWithSuffix("ref"), Source: src.BalanceID, Destination: dst.BalanceID, Amount: 100, Precision: 1, Currency: "USD", Status: StatusQueued}
	setTransactionMetadata(intent)
	intent.CreatedAt = time.Now().Add(-3 * time.Hour)
	_, err := ds.RecordTransaction(t.Context(), intent)
	require.NoError(t, err)
	chainAll(t, ds)
	for _, key := range []string{"allow_overdraft", "overdraft_limit", "inflight", "atomic", "skip_queue", "skip_balance_update", "queue_action", "QUEUED_PARENT_TRANSACTION", "queue_lane", "recovery_attempts", "inflight_commit_date"} {
		_, err = b.UpdateMetadata(t.Context(), intent.TransactionID, map[string]interface{}{key: true})
		require.ErrorContains(t, err, "reserved transaction execution metadata key")
	}
	_, err = b.UpdateMetadata(t.Context(), intent.TransactionID, map[string]interface{}{"customer_note": "allowed"})
	require.NoError(t, err)
	// Simulate a pre-fix client / direct metadata SQL write. Neither recovery nor
	// a stale worker payload can turn this into authorization to overdraw.
	_, err = ds.Conn.Exec(`UPDATE ledgerforge.transactions SET meta_data=meta_data || '{"allow_overdraft":true,"inflight":true}' WHERE transaction_id=$1`, intent.TransactionID)
	require.NoError(t, err)
	loaded, err := ds.GetTransaction(t.Context(), intent.TransactionID)
	require.NoError(t, err)
	require.False(t, loaded.AllowOverdraft)
	require.False(t, loaded.Inflight)
	require.Error(t, NewQueuedTransactionRecoveryProcessor(b).processStuckTransaction(t.Context(), loaded))
	copy := createQueueCopy(intent, intent.Reference)
	copy.AllowOverdraft = true
	_, err = b.ProcessQueuedTransaction(t.Context(), copy, false)
	require.Error(t, err)
	balance, err := ds.GetBalanceByIDLite(src.BalanceID)
	require.NoError(t, err)
	require.Equal(t, "0", balance.Balance.String())
	result, err := ds.VerifyChainSnapshot(t.Context(), nil)
	require.NoError(t, err)
	require.True(t, result.Verified, "%+v", result.Tampering)
}

func TestAuditCommitsExecutionControlsAndCap(t *testing.T) {
	for _, assignment := range []string{"overdraft_limit=0", "queue_action='COMMIT'", "execution_controls=jsonb_set(execution_controls,'{allow_overdraft}','true')", "execution_controls=jsonb_set(execution_controls,'{skip_queue}','true')"} {
		t.Run(assignment, func(t *testing.T) {
			_, ds := isolatedIntegrityEngine(t)
			src, dst := newBalancePair(t, ds)
			intent := &model.Transaction{TransactionID: model.GenerateUUIDWithSuffix("txn"), Reference: model.GenerateUUIDWithSuffix("ref"), Source: src.BalanceID, Destination: dst.BalanceID, Amount: 100, Precision: 1, Currency: "USD", Status: StatusQueued, OverdraftLimit: 5}
			setTransactionMetadata(intent)
			_, err := ds.RecordTransaction(t.Context(), intent)
			require.NoError(t, err)
			chainAll(t, ds)
			roots, err := ds.ExportChainCheckpoints(t.Context())
			require.NoError(t, err)
			_, err = ds.Conn.Exec(`UPDATE ledgerforge.transactions SET `+assignment+` WHERE transaction_id=$1`, intent.TransactionID)
			require.ErrorContains(t, err, "Cannot modify transaction data")
			tx, err := ds.Conn.Begin()
			require.NoError(t, err)
			_, err = tx.Exec(`SET LOCAL session_replication_role=replica`)
			require.NoError(t, err)
			_, err = tx.Exec(`UPDATE ledgerforge.transactions SET `+assignment+` WHERE transaction_id=$1`, intent.TransactionID)
			require.NoError(t, err)
			require.NoError(t, tx.Commit())
			result, err := ds.VerifyChainSnapshotWithAnchors(t.Context(), nil, roots)
			require.NoError(t, err)
			require.False(t, result.Verified)
			hashFinding, journalFinding := false, false
			for _, issue := range result.Tampering {
				if issue.Sequence > 0 {
					hashFinding = true
				}
				if issue.Reason == "live financial data differs from INSERT journal evidence" {
					journalFinding = true
				}
			}
			require.True(t, hashFinding, "v3 must commit controls independently of journal: %+v", result.Tampering)
			require.True(t, journalFinding, "%+v", result.Tampering)
		})
	}
}

func TestAuditSeededMainAndV1Upgrade(t *testing.T) {
	for _, scenario := range []string{"main", "dropped historical rate", "v1"} {
		t.Run(scenario, func(t *testing.T) {
			_, ds := isolatedIntegrityEngine(t)
			src, dst := newBalancePair(t, ds)
			source := migrate.EmbedFileSystemMigrationSource{FileSystem: SQLFiles, Root: "sql"}
			_, err := migrate.ExecVersion(ds.Conn, "postgres", source, migrate.Down, 1780963200)
			require.NoError(t, err)
			id := model.GenerateUUIDWithSuffix("txn")
			_, err = ds.Conn.Exec(`INSERT INTO ledgerforge.transactions (transaction_id,parent_transaction,source,destination,reference,amount,precise_amount,precision,currency,status,rate,created_at,meta_data) VALUES ($1,'',$2,$3,$1,100,10000,100,'USD','APPLIED',1.25,'2026-01-01','{"allow_overdraft":true}')`, id, src.BalanceID, dst.BalanceID)
			require.NoError(t, err)
			var oldJournal string
			require.NoError(t, ds.Conn.QueryRow(`SELECT new_data::text FROM ledgerforge.transaction_journal WHERE transaction_id=$1 AND action_type='INSERT'`, id).Scan(&oldJournal))
			if scenario == "dropped historical rate" {
				_, err = ds.Conn.Exec(`ALTER TABLE ledgerforge.transactions DROP COLUMN rate`)
				require.NoError(t, err)
			}
			if scenario == "v1" {
				_, err = migrate.ExecVersion(ds.Conn, "postgres", source, migrate.Up, 1781162329)
				require.NoError(t, err)
				row := model.ChainRow{TransactionID: id, Source: src.BalanceID, Destination: dst.BalanceID, Reference: id, Amount: "100", PreciseAmount: "10000", Currency: "USD", Status: StatusApplied, CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
				hash := model.ComputeChainHash(model.ChainGenesisHash, row)
				_, err = ds.Conn.Exec(`UPDATE ledgerforge.transactions SET chain_seq=1,chain_prev_hash=$1,chain_hash=$2 WHERE transaction_id=$3`, model.ChainGenesisHash, hash, id)
				require.NoError(t, err)
				_, err = ds.Conn.Exec(`UPDATE ledgerforge.chain_state SET last_seq=1,head_hash=$1 WHERE chain_key='global'`, hash)
				require.NoError(t, err)
			}
			var heapBefore, updatesBefore int64
			require.NoError(t, ds.Conn.QueryRow(`SELECT pg_relation_filenode('ledgerforge.transactions')`).Scan(&heapBefore))
			require.NoError(t, ds.Conn.QueryRow(`SELECT count(*) FROM ledgerforge.transaction_journal WHERE action_type='UPDATE'`).Scan(&updatesBefore))
			_, err = migrate.Exec(ds.Conn, "postgres", source, migrate.Up)
			require.NoError(t, err)
			var heapAfter, updatesAfter int64
			require.NoError(t, ds.Conn.QueryRow(`SELECT pg_relation_filenode('ledgerforge.transactions')`).Scan(&heapAfter))
			require.NoError(t, ds.Conn.QueryRow(`SELECT count(*) FROM ledgerforge.transaction_journal WHERE action_type='UPDATE'`).Scan(&updatesAfter))
			require.Equal(t, heapBefore, heapAfter, "upgrade must not rewrite the transaction heap")
			require.Equal(t, updatesBefore, updatesAfter, "upgrade must not backfill every transaction")
			var currentJournal string
			require.NoError(t, ds.Conn.QueryRow(`SELECT new_data::text FROM ledgerforge.transaction_journal WHERE transaction_id=$1 AND action_type='INSERT'`, id).Scan(&currentJournal))
			require.Equal(t, oldJournal, currentJournal)
			if scenario == "v1" {
				var version *int
				require.NoError(t, ds.Conn.QueryRow(`SELECT chain_version FROM ledgerforge.transactions WHERE transaction_id=$1`, id).Scan(&version))
				require.Nil(t, version, "upgrade must not rewrite every v1 row")
			}
			chainAll(t, ds)
			result, err := ds.VerifyChainSnapshot(context.Background(), nil)
			require.NoError(t, err)
			require.True(t, result.Verified, "scenario=%s %+v", scenario, result.Tampering)
			// Transitional allowance is bounded to the recorded restored default.
			tx, err := ds.Conn.Begin()
			require.NoError(t, err)
			_, err = tx.Exec(`SET LOCAL session_replication_role=replica`)
			require.NoError(t, err)
			_, err = tx.Exec(`UPDATE ledgerforge.transactions SET rate=2 WHERE transaction_id=$1`, id)
			require.NoError(t, err)
			require.NoError(t, tx.Commit())
			result, err = ds.VerifyChainSnapshot(context.Background(), nil)
			require.NoError(t, err)
			require.False(t, result.Verified)
		})
	}
}

func TestAuditSeededMainCustomerMetadataUpgrade(t *testing.T) {
	for _, metadata := range []string{
		`{"allow_overdraft":"merchant_label"}`,
		`{"inflight":7}`,
		`{"atomic":[]}`,
		`{"skip_queue":true}`,
		`{"queue_lane":true}`,
		`{"QUEUED_PARENT_TRANSACTION":9}`,
		`{"allow_overdraft":"true","inflight":"true","atomic":"true","skip_queue":{},"_shadow_for":[]}`,
		`{"allow_overdraft":null,"inflight":null,"atomic":null,"queue_lane":null,"QUEUED_PARENT_TRANSACTION":null}`,
		`{"allow_overdraft":false,"inflight":true,"atomic":true,"queue_lane":"hot"}`,
	} {
		t.Run(metadata, func(t *testing.T) {
			conn := openMigrationTestDB(t)
			set := migrate.MigrationSet{SchemaName: "ledgerforge"}
			_, err := set.Exec(conn, "postgres", migrate.EmbedFileSystemMigrationSource{FileSystem: mainMigrationFixtures, Root: mainMigrationRoot}, migrate.Up)
			require.NoError(t, err)
			seedMainMigrationLedger(t, conn)
			id := model.GenerateUUIDWithSuffix("txn")
			_, err = conn.Exec(`INSERT INTO ledgerforge.transactions
			(transaction_id,parent_transaction,reference,source,destination,amount,precise_amount,precision,currency,status,meta_data,description,hash,created_at,scheduled_for)
			VALUES ($1,'','legacy_customer_metadata_ref','main_src','main_dst',1,100,100,'USD','QUEUED',$2,'review','legacy_controls','1900-01-01','1900-01-01')`, id, metadata)
			require.NoError(t, err)
			before := migrationLedgerSnapshot(t, conn)
			var originalJournal string
			require.NoError(t, conn.QueryRow(`SELECT new_data::text FROM ledgerforge.transaction_journal WHERE transaction_id=$1 AND action_type='INSERT'`, id).Scan(&originalJournal))
			_, err = set.Exec(conn, "postgres", migrate.EmbedFileSystemMigrationSource{FileSystem: SQLFiles, Root: "sql"}, migrate.Up)
			require.NoError(t, err)
			require.JSONEq(t, before, migrationLedgerSnapshot(t, conn), "upgrade must retain raw customer metadata and INSERT evidence")

			b, base := newCoreTestLedgerForge(t)
			t.Cleanup(func() {
				require.NoError(t, b.Close())
			})
			ds := &database.Datasource{Conn: conn, Cache: base.(*database.Datasource).Cache}
			b.datasource = ds
			// Seal before hydration. The commitment must contain the raw legacy
			// values, even when their runtime meaning is the default false/empty.
			chainAll(t, ds)
			sealed, err := ds.GetChainedTransactionsAfter(t.Context(), 0, 100)
			require.NoError(t, err)
			var raw map[string]interface{}
			require.NoError(t, json.Unmarshal([]byte(metadata), &raw))
			evidence := map[string]interface{}{"economic_version": 0, "reserved_metadata": map[string]interface{}{}}
			for key, fallback := range map[string]interface{}{"allow_overdraft": false, "inflight": false, "atomic": false, "skip_queue": false, "queued_parent": "", "queue_lane": "normal", "shadow_for": ""} {
				metadataKey := key
				switch key {
				case "queued_parent":
					metadataKey = "QUEUED_PARENT_TRANSACTION"
				case "shadow_for":
					metadataKey = "_shadow_for"
				}
				value, exists := raw[metadataKey]
				if !exists {
					value = fallback
				}
				evidence[key] = value
			}
			encoded, err := json.Marshal(evidence)
			require.NoError(t, err)
			var canonicalEvidence string
			require.NoError(t, conn.QueryRow(`SELECT $1::jsonb::text`, string(encoded)).Scan(&canonicalEvidence))
			foundSeal := false
			for _, link := range sealed {
				if link.Row.TransactionID != id {
					continue
				}
				foundSeal = true
				require.Equal(t, 3, link.CanonicalVersion)
				require.Equal(t, canonicalEvidence, link.Row.ExecutionControls, "legacy normalization must never change canonical bytes")
				link.Row.ExecutionControls = canonicalEvidence
				expectedHash, err := model.ComputeChainHashVersion(3, link.ChainPrevHash, link.Row)
				require.NoError(t, err)
				require.Equal(t, expectedHash, link.ChainHash)
			}
			require.True(t, foundSeal)
			anchors, err := ds.ExportChainCheckpoints(t.Context())
			require.NoError(t, err)
			loaded, err := ds.GetTransaction(t.Context(), id)
			require.NoError(t, err)
			wantInflight, _ := raw["inflight"].(bool)
			wantAtomic, _ := raw["atomic"].(bool)
			require.False(t, loaded.AllowOverdraft)
			require.Equal(t, wantInflight, loaded.Inflight)
			require.Equal(t, wantAtomic, loaded.Atomic)
			loadedMetadata, err := json.Marshal(loaded.MetaData)
			require.NoError(t, err)
			require.JSONEq(t, metadata, string(loadedMetadata))
			stuck, err := ds.GetStuckQueuedTransactions(t.Context(), time.Second, 1)
			require.NoError(t, err)
			require.Len(t, stuck, 1, "legacy metadata must not hide accepted work from recovery")
			require.Equal(t, id, stuck[0].TransactionID)
			require.False(t, stuck[0].SkipQueue, "main never restored skip_queue from customer metadata")
			inflight, err := ds.GetInflightTransactionsByParentID(t.Context(), id, 100, 0)
			require.NoError(t, err)
			if wantInflight {
				require.Len(t, inflight, 1)
			} else {
				require.Empty(t, inflight, "customer string true is not an inflight flag")
			}
			wantLane := "normal"
			if lane, ok := raw["queue_lane"].(string); ok && lane == "hot" {
				wantLane = "hot"
			}
			require.Equal(t, wantLane, stuck[0].QueueLane)
			laneCount, err := ds.CountQueuedTransactionsForPairLane(t.Context(), "main_src", "main_dst", "USD", wantLane)
			require.NoError(t, err)
			if wantLane == "normal" {
				require.Equal(t, 2, laneCount)
			} else {
				require.Equal(t, 1, laneCount)
			}
			_, err = conn.Exec(`UPDATE ledgerforge.transactions SET meta_data='{"allow_overdraft":true,"inflight":false,"atomic":false}' WHERE transaction_id=$1`, id)
			require.NoError(t, err)
			sourceBefore, err := ds.GetBalanceByIDLite("main_src")
			require.NoError(t, err)
			require.NoError(t, NewQueuedTransactionRecoveryProcessor(b).processStuckTransaction(t.Context(), stuck[0]))
			children, err := ds.GetTransactionsByParent(t.Context(), id, 100, 0)
			require.NoError(t, err)
			require.Len(t, children, 1)
			expectedStatus := StatusApplied
			if wantInflight {
				expectedStatus = StatusInflight
			}
			require.Equal(t, expectedStatus, children[0].Status)
			require.False(t, children[0].AllowOverdraft)
			sourceAfter, err := ds.GetBalanceByIDLite("main_src")
			require.NoError(t, err)
			if wantInflight {
				require.Equal(t, sourceBefore.Balance, sourceAfter.Balance)
				require.Equal(t, new(big.Int).Add(sourceBefore.InflightDebitBalance, big.NewInt(100)), sourceAfter.InflightDebitBalance)
			} else {
				require.Equal(t, new(big.Int).Sub(sourceBefore.Balance, big.NewInt(100)), sourceAfter.Balance)
			}
			var currentJournal string
			require.NoError(t, conn.QueryRow(`SELECT new_data::text FROM ledgerforge.transaction_journal WHERE transaction_id=$1 AND action_type='INSERT'`, id).Scan(&currentJournal))
			require.Equal(t, originalJournal, currentJournal)
			chainAll(t, ds)
			audit, err := ds.VerifyChainSnapshotWithAnchors(t.Context(), nil, anchors)
			require.NoError(t, err)
			require.True(t, audit.Verified, "%+v", audit)
		})
	}
}

func TestAuditUpgradeValidationDoesNotBlockMetadataWriter(t *testing.T) {
	_, ds := isolatedIntegrityEngine(t)
	src, dst := newBalancePair(t, ds)
	intent := &model.Transaction{TransactionID: model.GenerateUUIDWithSuffix("txn"), Reference: model.GenerateUUIDWithSuffix("ref"), Source: src.BalanceID, Destination: dst.BalanceID, Amount: 100, Precision: 1, Currency: "USD", Status: StatusQueued}
	setTransactionMetadata(intent)
	_, err := ds.RecordTransaction(t.Context(), intent)
	require.NoError(t, err)
	var definition string
	require.NoError(t, ds.Conn.QueryRow(`SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid='ledgerforge.transactions'::regclass AND conname='transaction_chain_complete'`).Scan(&definition))
	_, err = ds.Conn.Exec(`ALTER TABLE ledgerforge.transactions DROP CONSTRAINT transaction_chain_complete`)
	require.NoError(t, err)
	_, err = ds.Conn.Exec(`ALTER TABLE ledgerforge.transactions ADD CONSTRAINT transaction_chain_complete ` + definition + ` NOT VALID`)
	require.NoError(t, err)
	writer, err := ds.Conn.Begin()
	require.NoError(t, err)
	defer func() { _ = writer.Rollback() }()
	_, err = writer.Exec(`UPDATE ledgerforge.transactions SET meta_data='{"customer_note":"concurrent"}' WHERE transaction_id=$1`, intent.TransactionID)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := ds.Conn.ExecContext(ctx, `ALTER TABLE ledgerforge.transactions VALIDATE CONSTRAINT transaction_chain_complete`)
		done <- err
	}()
	require.NoError(t, <-done, "separate validation must finish while the writer holds RowExclusiveLock")
	require.NoError(t, writer.Commit())
	var indexValid, constraintValid bool
	require.NoError(t, ds.Conn.QueryRow(`SELECT indisvalid FROM pg_index WHERE indexrelid='ledgerforge.idx_journal_insert_transaction'::regclass`).Scan(&indexValid))
	require.NoError(t, ds.Conn.QueryRow(`SELECT convalidated FROM pg_constraint WHERE conrelid='ledgerforge.transactions'::regclass AND conname='transaction_chain_complete'`).Scan(&constraintValid))
	require.True(t, indexValid)
	require.True(t, constraintValid)
}
