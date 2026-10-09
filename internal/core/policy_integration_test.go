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
	"errors"
	"math/big"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devaccuracy/ledgerforge/database"
	"github.com/devaccuracy/ledgerforge/internal/policy"
	"github.com/devaccuracy/ledgerforge/model"
	"github.com/stretchr/testify/require"
)

func policyCoreSetup(t *testing.T, max, daily, threshold string) (*LedgerForge, *database.Datasource, context.Context, context.Context, *model.Balance, *model.Balance) {
	t.Helper()
	l, ds := newCoreTestLedgerForge(t)
	d := ds.(*database.Datasource)
	source, destination := newBalancePair(t, ds)
	master := policy.WithPrincipal(context.Background(), policy.Principal{Master: true})
	key := model.GenerateUUIDWithSuffix("policy_agent")
	agent := policy.WithPrincipal(context.Background(), policy.Principal{APIKeyID: key, Scopes: []string{"transactions:write", "approvals:write"}})
	_, err := l.PutKeyPolicy(master, model.KeyPolicy{APIKeyID: key, Rules: []model.SpendingRule{{Currency: "USD", Precision: 100, MaxTransaction: max, DailyLimit: daily, ApprovalAbove: threshold}}})
	require.NoError(t, err)
	return l, d, agent, master, source, destination
}

func policyDraft(source, destination *model.Balance, minor int64) *model.Transaction {
	return &model.Transaction{Reference: model.GenerateUUIDWithSuffix("policy_ref"), Source: source.BalanceID, Destination: destination.BalanceID, Currency: "USD", Precision: 100, PreciseAmount: big.NewInt(minor), AllowOverdraft: true, SkipQueue: true}
}

func policyUsed(t *testing.T, l *LedgerForge, agent context.Context) string {
	t.Helper()
	c, err := l.PolicyCapacity(agent, policy.FromContext(agent).APIKeyID)
	require.NoError(t, err)
	return c[0].Used
}

func TestPolicyCoreApprovalAndLegacy(t *testing.T) {
	l, _, agent, master, source, destination := policyCoreSetup(t, "100", "200", "40")
	draft := policyDraft(source, destination, 60)
	draft.InflightCommitDate = time.Now().Add(time.Hour)
	held, err := l.QueueTransaction(agent, draft)
	require.NoError(t, err)
	require.Equal(t, StatusInflight, held.Status)
	require.Equal(t, "pending", held.MetaData["policy_approval"])
	require.True(t, held.InflightCommitDate.IsZero())
	_, err = l.CommitInflightTransaction(agent, held.TransactionID, big.NewInt(0))
	require.ErrorIs(t, err, policy.ErrApprovalRequired)
	_, err = l.CommitInflightTransactionWithQueue(agent, held.TransactionID, big.NewInt(0))
	require.ErrorIs(t, err, policy.ErrApprovalRequired)
	_, err = l.QueueInflightAction(agent, held.TransactionID, big.NewInt(0), InflightActionCommit)
	require.ErrorIs(t, err, policy.ErrApprovalRequired)
	admission := held.MetaData["policy_admission_id"].(string)
	if ds, ok := l.datasource.(*database.Datasource); ok {
		require.NoError(t, ds.UpdateTransactionMetadata(context.Background(), held.TransactionID, map[string]interface{}{"policy_approval": "approved", "policy_admission_id": "forged"}))
	}
	_, err = l.CommitInflightTransaction(agent, held.TransactionID, big.NewInt(0))
	require.ErrorIs(t, err, policy.ErrApprovalRequired)
	_, err = l.DecidePolicyApproval(agent, admission, true)
	require.ErrorIs(t, err, policy.ErrDenied)
	approved, err := l.DecidePolicyApproval(master, admission, true)
	require.NoError(t, err)
	require.Equal(t, "approved", approved.Decision)
	_, err = l.DecidePolicyApproval(master, admission, true)
	require.NoError(t, err)
	require.Equal(t, "60", policyUsed(t, l, agent))
	// Authenticated keys with no policy keep the ordinary posting/hold contract.
	legacy := policy.WithPrincipal(context.Background(), policy.Principal{APIKeyID: model.GenerateUUIDWithSuffix("legacy")})
	applied, err := l.QueueTransaction(legacy, policyDraft(source, destination, 300))
	require.NoError(t, err)
	require.Equal(t, StatusApplied, applied.Status)
	normal := policyDraft(source, destination, 10)
	normal.Inflight = true
	hold, err := l.QueueTransaction(legacy, normal)
	require.NoError(t, err)
	_, err = l.CommitInflightTransaction(legacy, hold.TransactionID, big.NewInt(0))
	require.NoError(t, err)
}

func TestPolicyCorePartialCommitRejectAndRefund(t *testing.T) {
	l, d, agent, master, source, destination := policyCoreSetup(t, "100", "100", "40")
	held, err := l.QueueTransaction(agent, policyDraft(source, destination, 60))
	require.NoError(t, err)
	_, err = d.DecidePolicyApproval(master, held.MetaData["policy_admission_id"].(string), policy.FromContext(master), true)
	require.NoError(t, err)
	_, err = l.CommitInflightTransaction(agent, held.TransactionID, big.NewInt(20))
	require.NoError(t, err)
	require.Equal(t, "60", policyUsed(t, l, agent))
	_, err = l.VoidInflightTransaction(agent, held.TransactionID)
	require.NoError(t, err)
	require.Equal(t, "20", policyUsed(t, l, agent))
	rejected, err := l.QueueTransaction(agent, policyDraft(source, destination, 60))
	require.NoError(t, err)
	_, err = l.DecidePolicyApproval(master, rejected.MetaData["policy_admission_id"].(string), false)
	require.NoError(t, err)
	require.Equal(t, "20", policyUsed(t, l, agent))
	_, err = l.CommitInflightTransaction(agent, rejected.TransactionID, big.NewInt(0))
	require.Error(t, err)
	// Refunds are new debits subject to the initiating identity's current policy.
	original := recordAppliedTxn(t, l, source.BalanceID, destination.BalanceID, 1)
	p, err := l.GetKeyPolicy(master, policy.FromContext(agent).APIKeyID)
	require.NoError(t, err)
	p.Rules[0].MaxTransaction = "10"
	_, err = l.PutKeyPolicy(master, *p)
	require.NoError(t, err)
	_, err = l.RefundTransaction(agent, original.TransactionID, true)
	require.ErrorIs(t, err, policy.ErrDenied)
}

func TestPolicyCoreSplitBulkAndFailure(t *testing.T) {
	l, _, agent, master, source, destination := policyCoreSetup(t, "70", "200", "40")
	split := policyDraft(source, destination, 80)
	split.Destinations = []model.Distribution{{Identifier: destination.BalanceID, Distribution: "50%"}, {Identifier: source.BalanceID, Distribution: "50%"}}
	_, err := l.QueueTransaction(agent, split)
	require.ErrorIs(t, err, policy.ErrDenied)
	_, err = l.CreateBulkTransactions(agent, &model.BulkTransactionRequest{Transactions: []*model.Transaction{policyDraft(source, destination, 40), policyDraft(source, destination, 40)}, SkipQueue: true})
	require.ErrorIs(t, err, policy.ErrDenied)
	require.Equal(t, "0", policyUsed(t, l, agent))
	// Bulk aggregate crosses the threshold even when each leg does not.
	result, err := l.CreateBulkTransactions(agent, &model.BulkTransactionRequest{Transactions: []*model.Transaction{policyDraft(source, destination, 30), policyDraft(source, destination, 30)}, SkipQueue: true})
	require.NoError(t, err)
	require.Equal(t, "pending_approval", result.Status)
	pending, err := l.ListPolicyApprovals(policy.WithPrincipal(context.Background(), policy.Principal{APIKeyID: policy.FromContext(agent).APIKeyID}))
	require.NoError(t, err)
	require.Len(t, pending, 1)
	_, err = l.DecidePolicyApproval(master, pending[0].ID, false)
	require.NoError(t, err)
	require.Equal(t, "0", policyUsed(t, l, agent))
	// A failed direct posting consumes its reference before releasing capacity.
	failed := policyDraft(source, destination, 10)
	failed.AllowOverdraft = false
	_, err = l.RecordTransaction(agent, failed)
	require.Error(t, err)
	require.Equal(t, "0", policyUsed(t, l, agent))
}

func TestPolicyCoreDurableQueueAdmission(t *testing.T) {
	l, d, agent, _, source, destination := policyCoreSetup(t, "100", "100", "40")
	draft := policyDraft(source, destination, 60)
	draft.SkipQueue = false
	queued, err := l.QueueTransaction(agent, draft)
	require.NoError(t, err)
	require.Eventually(t, func() bool { _, e := d.GetTransactionByRef(context.Background(), queued.Reference); return e == nil }, 5*time.Second, 10*time.Millisecond)
	persisted, err := d.GetTransactionByRef(context.Background(), queued.Reference)
	require.NoError(t, err)
	full, err := d.GetTransaction(context.Background(), persisted.TransactionID)
	require.NoError(t, err)
	worker := createQueueCopy(full, queued.Reference)
	// No trusted principal or caller metadata is needed: the binding is durable.
	worker.MetaData = nil
	worker.Inflight = false
	worker.AllowOverdraft = true
	recorded, err := l.ProcessQueuedTransaction(context.Background(), worker, false)
	require.NoError(t, err)
	require.Equal(t, StatusInflight, recorded.Status)
	_, err = l.CommitInflightTransaction(context.Background(), recorded.TransactionID, big.NewInt(0))
	require.ErrorIs(t, err, policy.ErrApprovalRequired)
	tampered := createQueueCopy(full, queued.Reference)
	tampered.PreciseAmount = big.NewInt(61)
	_, err = l.ProcessQueuedTransaction(context.Background(), tampered, false)
	require.ErrorIs(t, err, policy.ErrIdentityConflict)
}

func TestPolicyCoreParallelRequestsCannotOverspend(t *testing.T) {
	l, ds, agent, _, source, destination := policyCoreSetup(t, "100", "100", "")
	l.config.Transaction.LockWaitTimeout = 5 * time.Second
	var wg sync.WaitGroup
	start := make(chan struct{})
	var accepted atomic.Int32
	failures := make(chan error, 40)
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := l.QueueTransaction(agent, policyDraft(source, destination, 11))
			if err == nil {
				accepted.Add(1)
			} else if !errors.Is(err, policy.ErrDenied) {
				failures <- err
			}
		}()
	}
	close(start)
	wg.Wait()
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	require.Equal(t, int32(9), accepted.Load())
	require.Equal(t, "99", policyUsed(t, l, agent))
	balance, err := ds.GetBalanceByIDLite(destination.BalanceID)
	require.NoError(t, err)
	require.Equal(t, "99", balance.Balance.String())
}

func TestPolicyCoreSplitHoldAndAggregateRefund(t *testing.T) {
	l, ds, agent, master, source, destination := policyCoreSetup(t, "100", "200", "40")
	second, err := ds.CreateBalance(model.Balance{Currency: "USD", LedgerID: "general_ledger_id"})
	require.NoError(t, err)
	split := policyDraft(source, destination, 60)
	split.Destinations = []model.Distribution{{Identifier: destination.BalanceID, Distribution: "50%"}, {Identifier: second.BalanceID, Distribution: "50%"}}
	held, err := l.QueueTransaction(agent, split)
	require.NoError(t, err)
	require.Equal(t, StatusInflight, held.Status)
	require.Equal(t, "60", policyUsed(t, l, agent))
	_, err = l.DecidePolicyApproval(master, held.MetaData["policy_admission_id"].(string), false)
	require.NoError(t, err)
	require.Equal(t, "0", policyUsed(t, l, agent))
	// One parent refund must authorize its aggregate, even if every leg is small.
	bulk, err := l.CreateBulkTransactions(context.Background(), &model.BulkTransactionRequest{Transactions: []*model.Transaction{policyDraft(source, destination, 30), policyDraft(source, &second, 30)}, SkipQueue: true})
	require.NoError(t, err)
	p, err := l.GetKeyPolicy(master, policy.FromContext(agent).APIKeyID)
	require.NoError(t, err)
	p.Rules[0].MaxTransaction = "50"
	_, err = l.PutKeyPolicy(master, *p)
	require.NoError(t, err)
	_, err = l.ProcessRefundsInBatches(agent, bulk.BatchID, RefundOptions{SkipQueue: true})
	require.ErrorIs(t, err, policy.ErrDenied)
	require.Equal(t, "0", policyUsed(t, l, agent))
	p.Rules[0].MaxTransaction = "100"
	_, err = l.PutKeyPolicy(master, *p)
	require.NoError(t, err)
	refunds, err := l.ProcessRefundsInBatches(agent, bulk.BatchID, RefundOptions{SkipQueue: true})
	require.NoError(t, err)
	require.Len(t, refunds, 2)
	for _, refund := range refunds {
		require.Equal(t, StatusInflight, refund.Status)
		require.Equal(t, "pending", refund.MetaData["policy_approval"])
	}
	_, err = l.DecidePolicyApproval(master, refunds[0].MetaData["policy_admission_id"].(string), false)
	require.NoError(t, err)
	require.Equal(t, "0", policyUsed(t, l, agent))
}

func TestPolicyCoreDatabaseRejectsUnmarkedAdmissionBypass(t *testing.T) {
	l, ds, agent, _, source, destination := policyCoreSetup(t, "100", "100", "40")
	draft := policyDraft(source, destination, 60)
	setTransactionMetadata(draft)
	leg, err := l.policyLeg(agent, draft)
	require.NoError(t, err)
	_, err = ds.AdmitPolicy(agent, policy.FromContext(agent).APIKeyID, draft.Reference, []model.PolicyLeg{leg})
	require.NoError(t, err)
	// Another key cannot race a pending admission with an unmarked posting.
	legacy := policy.WithPrincipal(context.Background(), policy.Principal{APIKeyID: model.GenerateUUIDWithSuffix("legacy")})
	bypass := policyDraft(source, destination, 60)
	bypass.Reference = draft.Reference
	_, err = l.QueueTransaction(legacy, bypass)
	require.Error(t, err)
	balance, err := ds.GetBalanceByIDLite(destination.BalanceID)
	require.NoError(t, err)
	require.Equal(t, "0", balance.Balance.String())
	require.Equal(t, "60", policyUsed(t, l, agent))
}

func TestPolicyCoreTerminalCapacityReleaseIsAtomic(t *testing.T) {
	l, ds, agent, _, source, destination := policyCoreSetup(t, "100", "100", "40")
	draft := policyDraft(source, destination, 60)
	setTransactionMetadata(draft)
	leg, err := l.policyLeg(agent, draft)
	require.NoError(t, err)
	a, err := ds.AdmitPolicy(agent, policy.FromContext(agent).APIKeyID, draft.Reference, []model.PolicyLeg{leg})
	require.NoError(t, err)
	policy.Apply(a, draft)
	draft.Status = StatusRejected
	_, err = ds.RecordTransaction(agent, draft)
	require.NoError(t, err)
	require.Equal(t, "0", policyUsed(t, l, agent))
	// Retrying a released admission cannot post against the freed capacity.
	_, err = ds.AdmitPolicy(agent, policy.FromContext(agent).APIKeyID, draft.Reference, []model.PolicyLeg{leg})
	require.ErrorIs(t, err, policy.ErrDenied)
}
