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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"

	"github.com/devaccuracy/ledgerforge/internal/policy"
	"github.com/devaccuracy/ledgerforge/model"
	"github.com/sirupsen/logrus"
)

func (l *LedgerForge) policyStore() policy.Store {
	store, _ := l.datasource.(policy.Store)
	return store
}

func (l *LedgerForge) effectiveKeyPolicy(ctx context.Context) (*model.KeyPolicy, error) {
	p := policy.FromContext(ctx)
	if p.Master || p.APIKeyID == "" || policy.IsInternal(ctx) {
		return nil, nil
	}
	store := l.policyStore()
	if store == nil {
		return nil, fmt.Errorf("policy storage is unavailable for authenticated transactions")
	}
	return store.GetKeyPolicy(ctx, p.APIKeyID)
}

func (l *LedgerForge) policyLeg(ctx context.Context, t *model.Transaction) (model.PolicyLeg, error) {
	source, destination, err := l.getSourceAndDestination(ctx, t)
	if err != nil {
		return model.PolicyLeg{}, err
	}
	t.Source = source.BalanceID
	t.Destination = destination.BalanceID
	if t.PreciseAmount == nil {
		t.PreciseAmount = model.ApplyPrecision(t)
	}
	if t.PreciseAmount == nil || t.PreciseAmount.Sign() < 0 {
		return model.PolicyLeg{}, policy.ErrDenied
	}
	return model.PolicyLeg{TransactionID: t.TransactionID, Reference: t.Reference, Source: t.Source, Destination: t.Destination, SourceLedger: source.LedgerID, DestinationLedger: destination.LedgerID, Currency: t.Currency, Precision: t.Precision, Amount: t.PreciseAmount.String()}, nil
}

func (l *LedgerForge) admitPolicyTransactions(ctx context.Context, identity string, txns []*model.Transaction) (*model.PolicyAdmission, error) {
	legs := make([]model.PolicyLeg, 0, len(txns))
	for _, t := range txns {
		leg, err := l.policyLeg(ctx, t)
		if err != nil {
			return nil, err
		}
		legs = append(legs, leg)
	}
	a, err := (policy.Engine{Store: l.policyStore()}).Admit(ctx, identity, legs)
	if err != nil {
		return nil, err
	}
	if a != nil {
		// A retry reuses the original transaction identities and reservation.
		ids := map[string]string{}
		for _, leg := range a.Legs {
			ids[leg.Reference] = leg.TransactionID
		}
		for _, t := range txns {
			t.TransactionID = ids[t.Reference]
			policy.Apply(a, t)
		}
	}
	return a, nil
}

func (l *LedgerForge) admitQueuedPolicy(ctx context.Context, t *model.Transaction, split []*model.Transaction) (context.Context, error) {
	if refund, ok := ctx.Value(policyRefundKey{}).(policyRefundAdmission); ok && refund.admission != nil {
		policy.Apply(refund.admission, t)
	}
	p, err := l.effectiveKeyPolicy(ctx)
	if err != nil {
		return ctx, err
	}
	if p == nil {
		if policyMarked(t) {
			legs := split
			if len(legs) == 0 {
				legs = []*model.Transaction{t}
			} else {
				updateSplitTransactions(legs, t.TransactionID, t.Reference)
			}
			for _, leg := range legs {
				if err := l.verifyPolicyExecution(ctx, leg); err != nil {
					return ctx, err
				}
			}
		}
		return ctx, nil
	}
	legs := split
	if len(legs) == 0 {
		legs = []*model.Transaction{t}
	} else {
		updateSplitTransactions(legs, t.TransactionID, t.Reference)
	}
	a, err := l.admitPolicyTransactions(ctx, t.Reference, legs)
	if err != nil {
		return ctx, err
	}
	policy.Apply(a, t)
	return policy.Internal(ctx), nil
}

func (l *LedgerForge) admitBulkPolicy(ctx context.Context, req *model.BulkTransactionRequest) (context.Context, error) {
	p, err := l.effectiveKeyPolicy(ctx)
	if err != nil {
		return ctx, err
	}
	if p == nil {
		return ctx, nil
	}
	legs := []*model.Transaction{}
	refs := []string{}
	for _, t := range req.Transactions {
		if t == nil {
			return ctx, fmt.Errorf("nil bulk transaction")
		}
		setTransactionMetadata(t)
		if err = t.ValidateAmount(); err != nil {
			return ctx, err
		}
		refs = append(refs, t.Reference)
		if len(t.Sources) > 0 || len(t.Destinations) > 0 {
			split, e := l.handleSplitTransactions(ctx, t)
			if e != nil {
				return ctx, e
			}
			updateSplitTransactions(split, t.TransactionID, t.Reference)
			legs = append(legs, split...)
		} else {
			legs = append(legs, t)
		}
	}
	raw, _ := json.Marshal(refs)
	sum := sha256.Sum256(raw)
	a, err := l.admitPolicyTransactions(ctx, "bulk:"+hex.EncodeToString(sum[:]), legs)
	if err != nil {
		return ctx, err
	}
	for _, t := range req.Transactions {
		policy.Apply(a, t)
	}
	if a != nil && a.Decision != "not_required" {
		req.Inflight = true
	}
	return policy.Internal(ctx), nil
}

func policyMarked(t *model.Transaction) bool {
	_, ok := t.MetaData["policy_admission_id"]
	return ok
}

func (l *LedgerForge) verifyPolicyExecution(ctx context.Context, t *model.Transaction) error {
	if !policyMarked(t) {
		return nil
	}
	if t.Status == StatusCommit {
		return l.checkPolicyCommit(ctx, t.ParentTransaction)
	}
	if t.Status == StatusVoid {
		return nil
	}
	store := l.policyStore()
	if store == nil {
		return fmt.Errorf("policy storage unavailable")
	}
	a, err := store.VerifyPolicyLeg(ctx, t)
	if err != nil {
		return err
	}
	if a == nil {
		return policy.ErrDenied
	}
	policy.Apply(a, t)
	for _, leg := range a.Legs {
		if leg.Reference == t.Reference {
			t.TransactionID = leg.TransactionID
		}
	}
	return nil
}

func (l *LedgerForge) admitDirectPolicy(ctx context.Context, t *model.Transaction) (context.Context, error) {
	clearCallerPolicyMetadata(ctx, t)
	p, err := l.effectiveKeyPolicy(ctx)
	if err != nil {
		return ctx, err
	}
	if p == nil {
		return ctx, nil
	}
	setTransactionMetadata(t)
	setTransactionStatus(t)
	if len(t.Sources) > 0 || len(t.Destinations) > 0 {
		return ctx, fmt.Errorf("direct policy recording requires resolved legs; use QueueTransaction for splits")
	}
	_, err = l.admitPolicyTransactions(ctx, t.Reference, []*model.Transaction{t})
	return policy.Internal(ctx), err
}

func (l *LedgerForge) checkPolicyCommit(ctx context.Context, id string) error {
	if store := l.policyStore(); store != nil {
		return store.CheckPolicyCommit(ctx, id)
	}
	return nil
}

func (l *LedgerForge) releasePolicyCapacity(ctx context.Context, id string) {
	if store := l.policyStore(); store != nil {
		if err := store.ReleasePolicyLeg(ctx, id); err != nil {
			logrus.WithError(err).WithField("transaction_id", id).Error("release policy reservation")
		}
	}
}

func (l *LedgerForge) GetKeyPolicy(ctx context.Context, key string) (*model.KeyPolicy, error) {
	p := policy.FromContext(ctx)
	if !p.CanManagePolicies() && p.APIKeyID != key {
		return nil, policy.ErrDenied
	}
	store := l.policyStore()
	if store == nil {
		return nil, fmt.Errorf("policy storage unavailable")
	}
	return store.GetKeyPolicy(ctx, key)
}

func (l *LedgerForge) PutKeyPolicy(ctx context.Context, p model.KeyPolicy) (*model.KeyPolicy, error) {
	if !policy.FromContext(ctx).CanManagePolicies() {
		return nil, policy.ErrDenied
	}
	store := l.policyStore()
	if store == nil {
		return nil, fmt.Errorf("policy storage unavailable")
	}
	return store.PutKeyPolicy(ctx, p)
}

func (l *LedgerForge) DeleteKeyPolicy(ctx context.Context, key string) error {
	if !policy.FromContext(ctx).CanManagePolicies() {
		return policy.ErrDenied
	}
	store := l.policyStore()
	if store == nil {
		return fmt.Errorf("policy storage unavailable")
	}
	return store.DeleteKeyPolicy(ctx, key)
}

func (l *LedgerForge) PolicyCapacity(ctx context.Context, key string) ([]model.PolicyCapacity, error) {
	p := policy.FromContext(ctx)
	if !p.CanManagePolicies() && p.APIKeyID != key {
		return nil, policy.ErrDenied
	}
	store := l.policyStore()
	if store == nil {
		return nil, fmt.Errorf("policy storage unavailable")
	}
	return store.PolicyCapacity(ctx, key)
}

func (l *LedgerForge) ListPolicyApprovals(ctx context.Context) ([]model.PolicyAdmission, error) {
	p := policy.FromContext(ctx)
	if !p.CanApprove() && p.APIKeyID == "" {
		return nil, policy.ErrDenied
	}
	key := p.APIKeyID
	if p.CanApprove() {
		key = ""
	}
	store := l.policyStore()
	if store == nil {
		return nil, fmt.Errorf("policy storage unavailable")
	}
	return store.ListPolicyApprovals(ctx, key)
}

func (l *LedgerForge) DecidePolicyApproval(ctx context.Context, id string, approve bool) (*model.PolicyAdmission, error) {
	store := l.policyStore()
	if store == nil {
		return nil, fmt.Errorf("policy storage unavailable")
	}
	a, err := store.DecidePolicyApproval(ctx, id, policy.FromContext(ctx), approve)
	if err != nil {
		return nil, err
	}
	holds, err := store.PolicyHoldIDs(ctx, id)
	if err != nil {
		return a, err
	}
	if len(holds) == 0 {
		return a, fmt.Errorf("approval hold has not been recorded yet; retry this decision")
	}
	for _, hold := range holds {
		if approve {
			_, err = l.CommitInflightTransactionWithRef(policy.Internal(ctx), hold, big.NewInt(0), "policy_approve_"+id+"_"+hold)
		} else {
			_, err = l.VoidInflightTransactionWithRef(policy.Internal(ctx), hold, "policy_reject_"+id+"_"+hold)
		}
		if err != nil {
			// A completed child makes decision retries idempotent.
			if approve && classifyInflightError(err) == "ALREADY_VOIDED" {
				return a, err
			}
			if l.isTerminalInflightError(err) {
				continue
			}
			return a, err
		}
	}
	return a, nil
}

func clearCallerPolicyMetadata(ctx context.Context, t *model.Transaction) {
	if !policy.IsInternal(ctx) && t.MetaData != nil {
		delete(t.MetaData, "policy_admission_id")
		delete(t.MetaData, "policy_approval")
	}
}

func (l *LedgerForge) verifyQueuedPolicy(ctx context.Context, t *model.Transaction) error {
	store := l.policyStore()
	if store == nil {
		return nil
	}
	a, err := store.VerifyPolicyLeg(ctx, t)
	if err != nil {
		return err
	}
	policy.Apply(a, t)
	return nil
}

func (l *LedgerForge) failPolicyTransactions(ctx context.Context, txns []*model.Transaction) {
	for _, t := range txns {
		if !policyMarked(t) {
			continue
		}
		// A terminal row consumes the unique posting reference before capacity is
		// released, preventing a concurrent retry from posting after the release.
		rejected := cloneTransactionForAsync(t)
		rejected.Status = StatusRejected
		rejected.Atomic = false
		model.ApplyPrecision(rejected)
		if _, err := l.datasource.RecordTransaction(ctx, rejected); err != nil {
			if !IsDuplicateReferenceError(err) {
				logrus.WithError(err).Error("persist failed policy admission")
			}
			continue
		}
		l.releasePolicyCapacity(ctx, t.TransactionID)
	}
}

func policyAsyncTransactions(t *model.Transaction, txns []*model.Transaction) []*model.Transaction {
	if policyMarked(t) {
		return cloneTransactionsForAsync(txns)
	}
	return txns
}

func (l *LedgerForge) failPolicyBulk(ctx context.Context, txns []*model.Transaction) {
	for _, t := range txns {
		if t == nil || !policyMarked(t) {
			continue
		}
		if len(t.Sources) == 0 && len(t.Destinations) == 0 {
			l.failPolicyTransactions(ctx, []*model.Transaction{t})
			continue
		}
		split, err := l.handleSplitTransactions(ctx, t)
		if err != nil {
			continue
		}
		updateSplitTransactions(split, t.TransactionID, t.Reference)
		for _, leg := range split {
			if err := l.verifyPolicyExecution(ctx, leg); err == nil {
				l.failPolicyTransactions(ctx, []*model.Transaction{leg})
			}
		}
	}
}

type policyRefundKey struct{}
type policyRefundAdmission struct {
	admission    *model.PolicyAdmission
	transactions []*model.Transaction
}

func (l *LedgerForge) admitRefundPolicy(ctx context.Context, parent string, opts RefundOptions) (context.Context, error) {
	p, err := l.effectiveKeyPolicy(ctx)
	if err != nil || p == nil {
		return ctx, err
	}
	txns := []*model.Transaction{}
	for offset := int64(0); ; offset += 100 {
		originals, err := l.datasource.GetRefundableTransactionsByParentID(ctx, parent, 100, offset)
		if err != nil {
			return ctx, err
		}
		for _, original := range originals {
			if err = l.validateTransactionForRefund(ctx, original); err != nil {
				if isTransactionAlreadyRefundedError(err) {
					continue
				}
				return ctx, err
			}
			refund := prepareRefundTransaction(cloneTransactionForAsync(original), opts)
			clearCallerPolicyMetadata(ctx, refund)
			setTransactionMetadata(refund)
			txns = append(txns, refund)
		}
		if len(originals) < 100 {
			break
		}
	}
	if len(txns) == 0 {
		return ctx, nil
	}
	sort.Slice(txns, func(i, j int) bool { return txns[i].Reference < txns[j].Reference })
	refs := []string{}
	for _, t := range txns {
		refs = append(refs, t.Reference)
	}
	raw, _ := json.Marshal(refs)
	sum := sha256.Sum256(raw)
	a, err := l.admitPolicyTransactions(ctx, "refund:"+parent+":"+hex.EncodeToString(sum[:]), txns)
	if err != nil {
		return ctx, err
	}
	ctx = context.WithValue(ctx, policyRefundKey{}, policyRefundAdmission{admission: a, transactions: txns})
	return policy.Internal(ctx), nil
}

func (l *LedgerForge) failPolicyRefund(ctx context.Context) {
	if refund, ok := ctx.Value(policyRefundKey{}).(policyRefundAdmission); ok {
		l.failPolicyTransactions(ctx, refund.transactions)
	}
}
