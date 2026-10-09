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
	"database/sql"
	"fmt"
	"math/big"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/devaccuracy/ledgerforge/database"
	"github.com/devaccuracy/ledgerforge/internal/integrity"
	"github.com/devaccuracy/ledgerforge/model"
	migrate "github.com/rubenv/sql-migrate"
	"github.com/stretchr/testify/require"
)

// Each audit test gets a migrated database in itest's private PostgreSQL. The
// shared suite intentionally creates corrupt records and bypasses triggers;
// those fixtures cannot be part of a clean, whole-ledger verification test.
func isolatedIntegrityEngine(t *testing.T) (*LedgerForge, *database.Datasource) {
	t.Helper()
	b, base := newCoreTestLedgerForge(t) // skips before opening services in -short
	admin := base.(*database.Datasource).Conn
	name := "integrity_" + strings.ReplaceAll(model.GenerateUUIDWithSuffix("db"), "-", "_")
	_, err := admin.ExecContext(context.Background(), `CREATE DATABASE `+name)
	require.NoError(t, err)
	conn, err := sql.Open("postgres", "postgres://postgres:password@localhost:5432/"+name+"?sslmode=disable")
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = b.Close()
		_ = conn.Close()
		_, err := admin.ExecContext(context.Background(), `DROP DATABASE `+name+` WITH (FORCE)`)
		require.NoError(t, err)
	})
	migrate.SetSchema("ledgerforge")
	_, err = migrate.Exec(conn, "postgres", migrate.EmbedFileSystemMigrationSource{FileSystem: SQLFiles, Root: "sql"}, migrate.Up)
	require.NoError(t, err)
	ds := &database.Datasource{Conn: conn, Cache: base.(*database.Datasource).Cache}
	b.datasource = ds
	return b, ds
}

type integerAccount struct{ credit, debit, inflightCredit, inflightDebit int64 }

func (a integerAccount) available() int64 { return a.credit - a.debit - a.inflightDebit }

type generatedHold struct {
	id                  string
	source, destination int
	remaining           int64
}
type generatedApplied struct {
	id                  string
	source, destination int
	amount              int64
	refunded            bool
}

func TestIntegrityGeneratedOperationSequences(t *testing.T) {
	b, ds := isolatedIntegrityEngine(t)
	ctx := context.Background()
	// Fixed seeds make failures reproducible while exploring different orders.
	for seed := int64(1); seed <= 12; seed++ {
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))
			accounts := make([]integerAccount, 3)
			ids := make([]string, 3)
			for i := range ids {
				bal, err := ds.CreateBalance(model.Balance{Currency: "USD", LedgerID: "general_ledger_id"})
				require.NoError(t, err)
				ids[i] = bal.BalanceID
			}
			record := func(src, dst int, amount int64, inflight, overdraft bool, reference string) (*model.Transaction, error) {
				txn := &model.Transaction{TransactionID: model.GenerateUUIDWithSuffix("txn"), Reference: reference, Source: ids[src], Destination: ids[dst], Amount: float64(amount), PreciseAmount: big.NewInt(amount), Precision: 1, Currency: "USD", SkipQueue: true, AllowOverdraft: overdraft, Inflight: inflight, Status: StatusApplied}
				if inflight {
					txn.Status = StatusInflight
				}
				return b.RecordTransaction(ctx, txn)
			}
			funding, err := record(0, 1, 1000, false, true, model.GenerateUUIDWithSuffix("ref"))
			require.NoError(t, err)
			accounts[0].debit = 1000
			accounts[1].credit = 1000
			applied := []generatedApplied{{id: funding.TransactionID, source: 0, destination: 1, amount: 1000, refunded: true}}
			holds := []generatedHold{}
			verify := func() {
				report, err := integrity.Verify(ctx, ds)
				require.NoError(t, err)
				require.True(t, report.Verified, "seed=%d issues=%+v", seed, report.Issues)
				for i, id := range ids {
					actual, err := ds.GetBalanceByIDLite(id)
					require.NoError(t, err)
					expected := accounts[i]
					require.Equal(t, big.NewInt(expected.credit-expected.debit), actual.Balance, "seed=%d balance=%d", seed, i)
					require.Equal(t, big.NewInt(expected.credit), actual.CreditBalance)
					require.Equal(t, big.NewInt(expected.debit), actual.DebitBalance)
					require.Equal(t, big.NewInt(expected.inflightCredit), actual.InflightCreditBalance)
					require.Equal(t, big.NewInt(expected.inflightDebit), actual.InflightDebitBalance)
				}
			}
			var covered [7]bool
			for step := 0; step < 48; step++ {
				// Cycle the first operations to guarantee coverage in every sequence.
				op := step % 7
				if step >= 7 {
					op = rng.Intn(7)
				}
				src, dst := 1, 2
				if rng.Intn(2) == 1 {
					src, dst = dst, src
				}
				if step < 7 {
					src, dst = 1, 2
				}
				amount := int64(rng.Intn(40) + 2)
				switch op {
				case 0, 1:
					inflight := op == 1
					txn, err := record(src, dst, amount, inflight, false, model.GenerateUUIDWithSuffix("ref"))
					if accounts[src].available() < amount {
						require.Error(t, err)
						break
					}
					require.NoError(t, err)
					covered[op] = true
					if inflight {
						accounts[src].inflightDebit += amount
						accounts[dst].inflightCredit += amount
						holds = append(holds, generatedHold{id: txn.TransactionID, source: src, destination: dst, remaining: amount})
					} else {
						accounts[src].debit += amount
						accounts[dst].credit += amount
						applied = append(applied, generatedApplied{id: txn.TransactionID, source: src, destination: dst, amount: amount})
					}
				case 2, 3:
					active := []int{}
					for i, h := range holds {
						if h.remaining > 0 {
							active = append(active, i)
						}
					}
					if len(active) == 0 {
						break
					}
					h := &holds[active[rng.Intn(len(active))]]
					released := h.remaining
					if op == 2 {
						released = int64(rng.Intn(int(h.remaining)) + 1)
						if h.remaining > 1 {
							released = int64(rng.Intn(int(h.remaining-1)) + 1)
						}
						txn, err := b.CommitInflightTransactionWithRef(ctx, h.id, big.NewInt(released), model.GenerateUUIDWithSuffix("ref"))
						require.NoError(t, err)
						accounts[h.source].debit += released
						accounts[h.destination].credit += released
						applied = append(applied, generatedApplied{id: txn.TransactionID, source: h.source, destination: h.destination, amount: released})
					} else {
						_, err := b.VoidInflightTransaction(ctx, h.id)
						require.NoError(t, err)
					}
					accounts[h.source].inflightDebit -= released
					accounts[h.destination].inflightCredit -= released
					h.remaining -= released
					covered[op] = true
				case 4:
					candidates := []int{}
					for i, a := range applied {
						if !a.refunded {
							candidates = append(candidates, i)
						}
					}
					if len(candidates) == 0 {
						break
					}
					a := &applied[candidates[rng.Intn(len(candidates))]]
					_, err := b.RefundTransaction(ctx, a.id, true)
					require.NoError(t, err)
					accounts[a.destination].debit += a.amount
					accounts[a.source].credit += a.amount
					a.refunded = true
					covered[op] = true
				case 5:
					// Retrying an existing durable reference must not add economic legs.
					a := applied[rng.Intn(len(applied))]
					original, err := ds.GetTransaction(ctx, a.id)
					require.NoError(t, err)
					_, err = record(a.source, a.destination, a.amount, false, true, original.Reference)
					require.Error(t, err)
					covered[op] = true
				case 6:
					// Insufficient funds and an oversized reservation commit both roll back.
					covered[op] = true
					_, err := record(src, dst, 100000, false, false, model.GenerateUUIDWithSuffix("ref"))
					require.Error(t, err)
					for _, h := range holds {
						if h.remaining > 0 {
							_, err := b.CommitInflightTransaction(ctx, h.id, big.NewInt(h.remaining+1))
							require.Error(t, err)
							break
						}
					}
				}
				verify()
			}
			for op, tested := range covered {
				require.True(t, tested, "operation %d was not covered", op)
			}
			verify()
		})
	}
	chainAll(t, ds)
	audit, err := ds.VerifyChainSnapshot(ctx, nil)
	require.NoError(t, err)
	require.True(t, audit.Verified, "%+v", audit.Tampering)
	require.Zero(t, audit.Pending)
}

func TestIntegritySnapshotDuringConcurrentTransfers(t *testing.T) {
	b, ds := isolatedIntegrityEngine(t)
	src, dst := newBalancePair(t, ds)
	ctx := context.Background()
	var wg sync.WaitGroup
	errors := make(chan error, 1)
	done := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(done)
		for i := 0; i < 35; i++ {
			txn := &model.Transaction{TransactionID: model.GenerateUUIDWithSuffix("txn"), Reference: model.GenerateUUIDWithSuffix("ref"), Source: src.BalanceID, Destination: dst.BalanceID, Amount: 1, PreciseAmount: big.NewInt(1), Precision: 1, Currency: "USD", AllowOverdraft: true, SkipQueue: true, Status: StatusApplied}
			if _, err := b.RecordTransaction(ctx, txn); err != nil {
				errors <- err
				return
			}
		}
	}()
	for i := 0; i < 35; i++ {
		report, err := integrity.Verify(ctx, ds)
		require.NoError(t, err)
		require.True(t, report.Verified, "snapshot saw torn economic state: %+v", report.Issues)
	}
	wg.Wait()
	select {
	case err := <-errors:
		require.NoError(t, err)
	default:
	}
	<-done
}

func TestAuditLateCommitAndConcurrentChainers(t *testing.T) {
	b, ds := isolatedIntegrityEngine(t)
	src, dst := newBalancePair(t, ds)
	ctx := context.Background()
	late, err := ds.Conn.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = late.Rollback() }()
	lateID := model.GenerateUUIDWithSuffix("txn")
	_, err = late.ExecContext(ctx, `INSERT INTO ledgerforge.transactions (transaction_id,reference,amount,precise_amount,precision,currency,status) VALUES ($1,$1,1,1,1,'USD','REJECTED')`, lateID)
	require.NoError(t, err)
	early := recordAppliedTxn(t, b, src.BalanceID, dst.BalanceID, 2)
	chainAll(t, ds)
	earlySeq, _ := assertSealed(t, ds, early.TransactionID)
	require.NoError(t, late.Commit())
	// A rolled-back lower ID also must not produce a sequence gap.
	rollback, err := ds.Conn.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = rollback.ExecContext(ctx, `INSERT INTO ledgerforge.transactions (transaction_id,status) VALUES ($1,'REJECTED')`, model.GenerateUUIDWithSuffix("txn"))
	require.NoError(t, err)
	require.NoError(t, rollback.Rollback())
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := ds.ChainPendingTransactions(ctx, time.Now().Add(time.Hour), 2)
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	lateSeq, _ := assertSealed(t, ds, lateID)
	require.Greater(t, lateSeq, earlySeq, "lower transaction ID committed late must be sealed after the higher ID")
	result, err := ds.VerifyChainSnapshot(ctx, nil)
	require.NoError(t, err)
	require.True(t, result.Verified, "%+v", result.Tampering)
	require.Zero(t, result.Pending)
	roots, err := ds.ExportChainCheckpoints(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, roots)
	require.Equal(t, result.HeadHash, roots[len(roots)-1].HeadHash)
}

func TestAuditDetectsMutationDeletionAndTruncation(t *testing.T) {
	for _, scenario := range []string{"amount", "precise amount", "tail deletion", "unchained deletion", "checkpoint", "truncation", "version"} {
		t.Run(scenario, func(t *testing.T) {
			b, ds := isolatedIntegrityEngine(t)
			src, dst := newBalancePair(t, ds)
			first := recordAppliedTxn(t, b, src.BalanceID, dst.BalanceID, 5)
			last := recordAppliedTxn(t, b, src.BalanceID, dst.BalanceID, 7)
			ctx := context.Background()
			if scenario != "unchained deletion" {
				chainAll(t, ds)
			}
			switch scenario {
			case "amount", "precise amount", "version":
				tx, err := ds.Conn.BeginTx(ctx, nil)
				require.NoError(t, err)
				defer func() { _ = tx.Rollback() }()
				_, err = tx.ExecContext(ctx, `SET LOCAL session_replication_role = replica`)
				require.NoError(t, err)
				switch scenario {
				case "amount":
					_, err = tx.ExecContext(ctx, `UPDATE ledgerforge.transactions SET amount = amount + 1 WHERE transaction_id = $1`, first.TransactionID)
				case "precise amount":
					_, err = tx.ExecContext(ctx, `UPDATE ledgerforge.transactions SET precise_amount = precise_amount + 1 WHERE transaction_id = $1`, first.TransactionID)
				default:
					_, err = tx.ExecContext(ctx, `UPDATE ledgerforge.transactions SET chain_version = 1 WHERE transaction_id = $1`, first.TransactionID)
				}
				require.NoError(t, err)
				require.NoError(t, tx.Commit())
			case "tail deletion", "unchained deletion":
				tx, err := ds.Conn.BeginTx(ctx, nil)
				require.NoError(t, err)
				defer func() { _ = tx.Rollback() }()
				_, err = tx.ExecContext(ctx, `SET LOCAL session_replication_role = replica`)
				require.NoError(t, err)
				_, err = tx.ExecContext(ctx, `DELETE FROM ledgerforge.transactions WHERE transaction_id = $1`, last.TransactionID)
				require.NoError(t, err)
				require.NoError(t, tx.Commit())
			case "checkpoint":
				_, err := ds.Conn.ExecContext(ctx, `UPDATE ledgerforge.chain_checkpoints SET head_hash = repeat('f',64)`)
				require.NoError(t, err)
			case "truncation":
				_, err := ds.Conn.ExecContext(ctx, `TRUNCATE ledgerforge.transactions CASCADE`)
				require.NoError(t, err)
			}
			result, err := ds.VerifyChainSnapshot(ctx, nil)
			require.NoError(t, err)
			require.False(t, result.Verified)
			require.NotEmpty(t, result.Tampering)
		})
	}
}

func TestAuditPendingIsNotTampering(t *testing.T) {
	b, ds := isolatedIntegrityEngine(t)
	src, dst := newBalancePair(t, ds)
	recordAppliedTxn(t, b, src.BalanceID, dst.BalanceID, 1)
	result, err := ds.VerifyChainSnapshot(context.Background(), nil)
	require.NoError(t, err)
	require.True(t, result.Verified)
	require.Equal(t, int64(1), result.Pending)
	require.Empty(t, result.Tampering)
}

func TestAuditMixedCanonicalVersions(t *testing.T) {
	b, ds := isolatedIntegrityEngine(t)
	src, dst := newBalancePair(t, ds)
	first := recordAppliedTxn(t, b, src.BalanceID, dst.BalanceID, 1)
	ctx := context.Background()
	snapshot, err := ds.ReadIntegritySnapshot(ctx)
	require.NoError(t, err)
	require.Len(t, snapshot.Transactions, 1)
	oldHash := model.ComputeChainHash(model.ChainGenesisHash, snapshot.Transactions[0])
	_, err = ds.Conn.ExecContext(ctx, `UPDATE ledgerforge.transactions SET chain_seq=1,chain_prev_hash=$1,chain_hash=$2,chain_version=1 WHERE transaction_id=$3`, model.ChainGenesisHash, oldHash, first.TransactionID)
	require.NoError(t, err)
	_, err = ds.Conn.ExecContext(ctx, `UPDATE ledgerforge.chain_state SET last_seq=1,head_hash=$1`, oldHash)
	require.NoError(t, err)
	second := recordAppliedTxn(t, b, src.BalanceID, dst.BalanceID, 2)
	snapshot, err = ds.ReadIntegritySnapshot(ctx)
	require.NoError(t, err)
	var secondRow model.ChainRow
	for _, row := range snapshot.Transactions {
		if row.TransactionID == second.TransactionID {
			secondRow = row
		}
	}
	v2hash, err := model.ComputeChainHashVersion(2, oldHash, secondRow)
	require.NoError(t, err)
	_, err = ds.Conn.ExecContext(ctx, `UPDATE ledgerforge.transactions SET chain_seq=2,chain_prev_hash=$1,chain_hash=$2,chain_version=2 WHERE transaction_id=$3`, oldHash, v2hash, second.TransactionID)
	require.NoError(t, err)
	_, err = ds.Conn.ExecContext(ctx, `UPDATE ledgerforge.chain_state SET last_seq=2,head_hash=$1`, v2hash)
	require.NoError(t, err)
	third := recordAppliedTxn(t, b, src.BalanceID, dst.BalanceID, 3)
	chainAll(t, ds)
	assertSealed(t, ds, third.TransactionID)
	assertSealed(t, ds, second.TransactionID)
	result, err := ds.VerifyChainSnapshot(ctx, nil)
	require.NoError(t, err)
	require.True(t, result.Verified, "%+v", result.Tampering)
	require.Equal(t, int64(3), result.LastSeq)
}

func TestAuditSnapshotWhileChainAdvances(t *testing.T) {
	b, ds := isolatedIntegrityEngine(t)
	src, dst := newBalancePair(t, ds)
	recordAppliedTxn(t, b, src.BalanceID, dst.BalanceID, 1)
	chainAll(t, ds)
	advanced := false
	result, err := ds.VerifyChainSnapshot(context.Background(), func(seq int64) {
		if advanced {
			return
		}
		advanced = true
		recordAppliedTxn(t, b, src.BalanceID, dst.BalanceID, 2)
		chainAll(t, ds)
	})
	require.NoError(t, err)
	require.True(t, advanced)
	require.True(t, result.Verified, "%+v", result.Tampering)
	require.Equal(t, int64(1), result.LastSeq)
	require.Zero(t, result.Pending)
	latest, err := ds.VerifyChainSnapshot(context.Background(), nil)
	require.NoError(t, err)
	require.True(t, latest.Verified)
	require.Equal(t, int64(2), latest.LastSeq)
}

func TestAuditSealedCommitmentsAreImmutable(t *testing.T) {
	b, ds := isolatedIntegrityEngine(t)
	src, dst := newBalancePair(t, ds)
	txn := recordAppliedTxn(t, b, src.BalanceID, dst.BalanceID, 1)
	chainAll(t, ds)
	for _, assignment := range []string{"id=id+1000", "parent_transaction='changed'", "rate=2", "effective_date=now() + interval '1 day'", "chain_hash=repeat('f',64)", "chain_version=1", "chain_seq=NULL,chain_prev_hash=NULL,chain_hash=NULL,chain_version=NULL"} {
		_, err := ds.Conn.ExecContext(context.Background(), `UPDATE ledgerforge.transactions SET `+assignment+` WHERE transaction_id=$1`, txn.TransactionID)
		require.Error(t, err, "sealed field mutation accepted: %s", assignment)
	}
	_, err := ds.Conn.ExecContext(context.Background(), `UPDATE ledgerforge.transactions SET meta_data='{"note":"mutable"}' WHERE transaction_id=$1`, txn.TransactionID)
	require.NoError(t, err)
	result, err := ds.VerifyChainSnapshot(context.Background(), nil)
	require.NoError(t, err)
	require.True(t, result.Verified, "%+v", result.Tampering)
}

func TestAuditExternalAnchors(t *testing.T) {
	b, ds := isolatedIntegrityEngine(t)
	src, dst := newBalancePair(t, ds)
	recordAppliedTxn(t, b, src.BalanceID, dst.BalanceID, 1)
	chainAll(t, ds)
	ctx := context.Background()
	anchors, err := ds.ExportChainCheckpoints(ctx)
	require.NoError(t, err)
	result, err := ds.VerifyChainSnapshotWithAnchors(ctx, nil, anchors)
	require.NoError(t, err)
	require.True(t, result.Verified)
	anchors[0].HeadHash = strings.Repeat("f", 64)
	result, err = ds.VerifyChainSnapshotWithAnchors(ctx, nil, anchors)
	require.NoError(t, err)
	require.False(t, result.Verified)
	require.Contains(t, result.Reason, "external checkpoint")
}
