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
	"fmt"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/devaccuracy/ledgerforge/database"
	"github.com/devaccuracy/ledgerforge/model"
	"github.com/stretchr/testify/require"
)

func lifecycleHold(t *testing.T, b *LedgerForge, ds database.IDataSource) *model.Transaction {
	t.Helper()
	src, dst := newBalancePair(t, ds)
	hold := &model.Transaction{
		TransactionID: model.GenerateUUIDWithSuffix("txn"),
		Reference:     model.GenerateUUIDWithSuffix("ref"),
		Source:        src.BalanceID, Destination: dst.BalanceID,
		PreciseAmount: big.NewInt(10000), Precision: 100, Rate: 1.25,
		Currency: "USD", AllowOverdraft: true, Inflight: true, Status: StatusInflight,
	}
	model.ApplyPrecision(hold)
	applied, err := b.RecordTransaction(context.Background(), hold)
	require.NoError(t, err)
	return applied
}

func lifecycleIntent(original *model.Transaction, amount int64) *model.Transaction {
	intent := cloneTransactionForAsync(original)
	intent.ParentTransaction = original.TransactionID
	intent.TransactionID = model.GenerateUUIDWithSuffix("txn")
	intent.Reference = model.GenerateUUIDWithSuffix("ref")
	intent.Status = StatusQueued
	intent.QueueAction = StatusCommit
	intent.Inflight = false
	intent.PreciseAmount = big.NewInt(amount)
	intent.CreatedAt = time.Now().UTC().Add(-3 * time.Hour)
	model.ApplyPrecision(intent)
	return intent
}

// These inserts deliberately bypass Redis to establish the PostgreSQL guarantee.
func TestLifecycleConcurrentRefundClaims(t *testing.T) {
	b, ds := newCoreTestLedgerForge(t)
	src, dst := newBalancePair(t, ds)
	original := recordAppliedTxn(t, b, src.BalanceID, dst.BalanceID, 100)
	const n = 12
	start := make(chan struct{})
	intents := make(chan *model.Transaction, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			intent := prepareRefundTransaction(original, RefundOptions{})
			setTransactionMetadata(intent)
			intent.Status = StatusQueued
			<-start
			if _, err := ds.RecordTransaction(context.Background(), intent); err == nil {
				intents <- intent
			}
		}()
	}
	close(start)
	wg.Wait()
	close(intents)
	require.Len(t, intents, 1)
	winner := <-intents
	conn := ds.(*database.Datasource).Conn
	var claim string
	require.NoError(t, conn.QueryRow("SELECT reversal_transaction FROM ledgerforge.refund_claims WHERE original_transaction=$1", original.TransactionID).Scan(&claim))
	require.Equal(t, winner.TransactionID, claim)
	_, err := b.ProcessQueuedTransaction(context.Background(), createQueueCopy(winner, winner.Reference), false)
	require.NoError(t, err)
	source, err := ds.GetBalanceByIDLite(original.Source)
	require.NoError(t, err)
	destination, err := ds.GetBalanceByIDLite(original.Destination)
	require.NoError(t, err)
	require.Equal(t, "0", source.Balance.String())
	require.Equal(t, "0", destination.Balance.String())
}

func TestLifecycleQueuedPartialCommits(t *testing.T) {
	b, ds := newCoreTestLedgerForge(t)
	hold := lifecycleHold(t, b, ds)
	first, err := b.CommitInflightTransactionWithQueue(context.Background(), hold.TransactionID, big.NewInt(6000))
	require.NoError(t, err)
	require.Equal(t, StatusQueued, first.Status)
	stored, err := ds.GetTransaction(context.Background(), first.TransactionID)
	require.NoError(t, err)
	require.Equal(t, StatusCommit, stored.QueueAction)
	require.Equal(t, hold.Rate, stored.Rate)
	_, err = b.CommitInflightTransactionWithQueue(context.Background(), hold.TransactionID, big.NewInt(5000))
	require.Error(t, err)
	second, err := b.CommitInflightTransactionWithQueue(context.Background(), hold.TransactionID, big.NewInt(4000))
	require.NoError(t, err)
	_, err = b.CommitInflightTransactionWithQueue(context.Background(), hold.TransactionID, big.NewInt(1))
	require.Error(t, err)
	_, err = b.VoidInflightTransaction(context.Background(), hold.TransactionID)
	require.Error(t, err)
	for _, intent := range []*model.Transaction{first, second} {
		_, err = b.ProcessQueuedTransaction(context.Background(), createQueueCopy(intent, intent.Reference), false)
		require.NoError(t, err)
	}
	source, err := ds.GetBalanceByIDLite(hold.Source)
	require.NoError(t, err)
	destination, err := ds.GetBalanceByIDLite(hold.Destination)
	require.NoError(t, err)
	require.Equal(t, "10000", source.DebitBalance.String())
	require.Equal(t, source.DebitBalance.String(), destination.CreditBalance.String())
	require.Equal(t, "0", source.InflightDebitBalance.String())
	require.Equal(t, "0", destination.InflightCreditBalance.String())
}

func TestLifecycleParallelPartialCommitReservations(t *testing.T) {
	b, ds := newCoreTestLedgerForge(t)
	hold := lifecycleHold(t, b, ds)
	const n = 12
	start := make(chan struct{})
	accepted := make(chan *model.Transaction, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			intent := lifecycleIntent(hold, 3000)
			<-start
			if _, err := ds.RecordTransaction(context.Background(), intent); err == nil {
				accepted <- intent
			}
		}()
	}
	close(start)
	wg.Wait()
	close(accepted)
	require.Len(t, accepted, 3)
	// Void releases only the unclaimed remainder, even before pending commits run.
	voided, err := b.VoidInflightTransaction(context.Background(), hold.TransactionID)
	require.NoError(t, err)
	require.Equal(t, "1000", voided.PreciseAmount.String())
	for intent := range accepted {
		_, err := b.ProcessQueuedTransaction(context.Background(), createQueueCopy(intent, intent.Reference), false)
		require.NoError(t, err)
	}
	source, err := ds.GetBalanceByIDLite(hold.Source)
	require.NoError(t, err)
	destination, err := ds.GetBalanceByIDLite(hold.Destination)
	require.NoError(t, err)
	require.Equal(t, "9000", source.DebitBalance.String())
	require.Equal(t, "9000", destination.CreditBalance.String())
	require.Equal(t, "0", source.InflightDebitBalance.String())
	require.Equal(t, "0", destination.InflightCreditBalance.String())
}

func TestLifecycleRefundEligibility(t *testing.T) {
	for _, status := range []string{StatusQueued, StatusScheduled, StatusInflight, StatusVoid, StatusRejected} {
		t.Run(status, func(t *testing.T) {
			err := (&LedgerForge{}).validateTransactionForRefund(context.Background(), &model.Transaction{Status: status})
			require.ErrorContains(t, err, "only APPLIED")
		})
	}
}

func TestLifecycleTransitionFailureLeavesBothLegsUnchanged(t *testing.T) {
	for _, status := range []string{StatusCommit, StatusVoid} {
		for _, amount := range []int64{-1, 0, 10} {
			t.Run(fmt.Sprintf("%s/%d", status, amount), func(t *testing.T) {
				source := &model.Balance{InflightDebitBalance: big.NewInt(20)}
				destination := &model.Balance{InflightCreditBalance: big.NewInt(5)}
				source.InitializeBalanceFields()
				destination.InitializeBalanceFields()
				err := (&LedgerForge{}).applyTransactionToBalances(context.Background(), []*model.Balance{source, destination}, &model.Transaction{Status: status, PreciseAmount: big.NewInt(amount), Precision: 1})
				require.Error(t, err)
				require.Equal(t, "20", source.InflightDebitBalance.String())
				require.Equal(t, "5", destination.InflightCreditBalance.String())
				require.Equal(t, "0", source.DebitBalance.String())
				require.Equal(t, "0", destination.CreditBalance.String())
			})
		}
	}
}

func TestLifecycleImmutabilityAndRefundRollback(t *testing.T) {
	b, ds := newCoreTestLedgerForge(t)
	src, dst := newBalancePair(t, ds)
	original := recordAppliedTxn(t, b, src.BalanceID, dst.BalanceID, 10)
	conn := ds.(*database.Datasource).Conn
	for _, change := range []string{
		"precise_amount=precise_amount+1", "precision=precision+1", "rate=rate+0.5",
		"parent_transaction='tampered'", "effective_date=NOW()+interval '1 day'",
		"id=id+100000000", "transaction_id='tampered'", "overdraft_limit=7", "queue_action='COMMIT'",
	} {
		_, err := conn.Exec("UPDATE ledgerforge.transactions SET "+change+" WHERE transaction_id=$1", original.TransactionID)
		require.Error(t, err, change)
	}
	_, err := conn.Exec("DELETE FROM ledgerforge.transactions WHERE transaction_id=$1", original.TransactionID)
	require.Error(t, err)
	_, err = conn.Exec(`UPDATE ledgerforge.transactions SET meta_data='{"note":"allowed"}'::jsonb WHERE transaction_id=$1`, original.TransactionID)
	require.NoError(t, err)
	// The claim and intent roll back together, allowing a retry.
	tx, err := conn.Begin()
	require.NoError(t, err)
	intent := prepareRefundTransaction(original, RefundOptions{})
	_, err = tx.Exec(`INSERT INTO ledgerforge.transactions (transaction_id,parent_transaction,reference,source,destination,amount,precise_amount,precision,currency,status) VALUES ($1,$2,$3,$4,$5,10,1000,100,'USD','QUEUED')`, intent.TransactionID, original.TransactionID, intent.Reference, intent.Source, intent.Destination)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())
	_, err = b.RefundTransaction(context.Background(), original.TransactionID, true)
	require.NoError(t, err)
}

func TestLifecycleRecoveryPreservesOverdraftCapAndCommitAction(t *testing.T) {
	b, ds := newCoreTestLedgerForge(t)
	src, dst := newBalancePair(t, ds)
	intent := &model.Transaction{
		TransactionID: model.GenerateUUIDWithSuffix("txn"), Reference: model.GenerateUUIDWithSuffix("ref"),
		Source: src.BalanceID, Destination: dst.BalanceID, Amount: 10, Precision: 100, Currency: "USD", Rate: 1.25,
		AllowOverdraft: true, OverdraftLimit: 5, Status: StatusQueued,
	}
	setTransactionMetadata(intent)
	intent.CreatedAt = time.Now().UTC().Add(-3 * time.Hour)
	_, err := ds.RecordTransaction(context.Background(), intent)
	require.NoError(t, err)
	loaded, err := ds.GetTransaction(context.Background(), intent.TransactionID)
	require.NoError(t, err)
	require.Equal(t, 5.0, loaded.OverdraftLimit)
	stuck, err := ds.GetStuckQueuedTransactions(context.Background(), 2*time.Hour, 10000)
	require.NoError(t, err)
	var restored *model.Transaction
	for _, row := range stuck {
		if row.TransactionID == intent.TransactionID {
			restored = row
			break
		}
	}
	require.NotNil(t, restored)
	require.Equal(t, 5.0, restored.OverdraftLimit)
	require.Equal(t, 1.25, restored.Rate)
	processor := NewQueuedTransactionRecoveryProcessor(b)
	for i := 0; i < 3; i++ {
		require.ErrorContains(t, processor.processStuckTransaction(context.Background(), restored), "overdraft limit")
	}
	require.NoError(t, processor.processStuckTransaction(context.Background(), restored))
	child, err := ds.GetTransactionByRef(context.Background(), intent.Reference+"_q")
	require.NoError(t, err)
	require.Equal(t, StatusRejected, child.Status)
	balance, err := ds.GetBalanceByIDLite(src.BalanceID)
	require.NoError(t, err)
	require.Equal(t, "0", balance.DebitBalance.String())

	hold := lifecycleHold(t, b, ds)
	pending := lifecycleIntent(hold, 4000)
	_, err = ds.RecordTransaction(context.Background(), pending)
	require.NoError(t, err)
	loaded, err = ds.GetTransaction(context.Background(), pending.TransactionID)
	require.NoError(t, err)
	require.NoError(t, processor.processStuckTransaction(context.Background(), loaded))
	balance, err = ds.GetBalanceByIDLite(hold.Source)
	require.NoError(t, err)
	require.Equal(t, "4000", balance.DebitBalance.String())
	require.Equal(t, "6000", balance.InflightDebitBalance.String())
}

func TestLifecycleQueuedAcceptanceIsDurable(t *testing.T) {
	b, ds := newCoreTestLedgerForge(t)
	src, dst := newBalancePair(t, ds)
	intent := &model.Transaction{Reference: model.GenerateUUIDWithSuffix("ref"), Source: src.BalanceID, Destination: dst.BalanceID, Amount: 10, Precision: 100, Currency: "USD", AllowOverdraft: true, OverdraftLimit: 15}
	accepted, err := b.QueueTransaction(context.Background(), intent)
	require.NoError(t, err)
	stored, err := ds.GetTransaction(context.Background(), accepted.TransactionID)
	require.NoError(t, err)
	require.Equal(t, StatusQueued, stored.Status)
	require.Equal(t, 15.0, stored.OverdraftLimit)
	duplicate := cloneTransactionForAsync(intent)
	duplicate.TransactionID = model.GenerateUUIDWithSuffix("txn")
	_, err = b.QueueTransaction(context.Background(), duplicate)
	require.Error(t, err, "failed persistence must not be acknowledged")
}

func TestLifecycleWorkerRecoveryOverlap(t *testing.T) {
	b, ds := newCoreTestLedgerForge(t)
	hold := lifecycleHold(t, b, ds)
	intent := lifecycleIntent(hold, 4000)
	_, err := ds.RecordTransaction(context.Background(), intent)
	require.NoError(t, err)
	start := make(chan struct{})
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		copy := createQueueCopy(intent, intent.Reference)
		go func() {
			<-start
			_, err := b.ProcessQueuedTransaction(context.Background(), copy, false)
			results <- err
		}()
	}
	close(start)
	successes := 0
	for i := 0; i < 2; i++ {
		if <-results == nil {
			successes++
		}
	}
	require.Equal(t, 1, successes)
	source, err := ds.GetBalanceByIDLite(hold.Source)
	require.NoError(t, err)
	destination, err := ds.GetBalanceByIDLite(hold.Destination)
	require.NoError(t, err)
	require.Equal(t, "4000", source.DebitBalance.String())
	require.Equal(t, "4000", destination.CreditBalance.String())
	require.Equal(t, "6000", source.InflightDebitBalance.String())
	require.Equal(t, "6000", destination.InflightCreditBalance.String())
	processor := NewQueuedTransactionRecoveryProcessor(b)
	require.NoError(t, processor.processStuckTransaction(context.Background(), intent))
}
