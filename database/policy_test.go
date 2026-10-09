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

package database

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devaccuracy/ledgerforge/internal/policy"
	"github.com/devaccuracy/ledgerforge/model"
	"github.com/stretchr/testify/require"
)

func policyTestStore(t *testing.T) (*Datasource, string) {
	t.Helper()
	d := openRealTestDB(t)
	key := model.GenerateUUIDWithSuffix("policy_test")
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = d.Conn.ExecContext(ctx, `DELETE FROM ledgerforge.policy_bindings WHERE admission_id IN (SELECT admission_id FROM ledgerforge.policy_admissions WHERE api_key_id=$1)`, key)
		_, _ = d.Conn.ExecContext(ctx, `DELETE FROM ledgerforge.policy_admissions WHERE api_key_id=$1`, key)
		_, _ = d.Conn.ExecContext(ctx, `DELETE FROM ledgerforge.policy_usage WHERE api_key_id=$1`, key)
		_, _ = d.Conn.ExecContext(ctx, `DELETE FROM ledgerforge.policy_rules WHERE api_key_id=$1`, key)
		_, _ = d.Conn.ExecContext(ctx, `DELETE FROM ledgerforge.key_policies WHERE api_key_id=$1`, key)
	})
	return &d, key
}

func policyTestLeg(ref, amount string) model.PolicyLeg {
	return model.PolicyLeg{TransactionID: model.GenerateUUIDWithSuffix("txn"), Reference: ref, Source: "source", Destination: "destination", SourceLedger: "ledger", DestinationLedger: "ledger", Currency: "USD", Precision: 100, Amount: amount}
}

func TestPolicyPostgresParallelLimitsAndRetry(t *testing.T) {
	d, key := policyTestStore(t)
	ctx := context.Background()
	_, err := d.PutKeyPolicy(ctx, model.KeyPolicy{APIKeyID: key, Rules: []model.SpendingRule{{Currency: "USD", Precision: 100, DailyLimit: "100"}}})
	require.NoError(t, err)
	var accepted atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	errors := make(chan error, 40)
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			ref := fmt.Sprintf("%s_%d", key, i)
			_, e := d.AdmitPolicy(ctx, key, ref, []model.PolicyLeg{policyTestLeg(ref, "11")})
			if e == nil {
				accepted.Add(1)
			} else if !isPolicyDenied(e) {
				errors <- e
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(errors)
	for e := range errors {
		require.NoError(t, e)
	}
	require.Equal(t, int32(9), accepted.Load())
	capacity, err := d.PolicyCapacity(ctx, key)
	require.NoError(t, err)
	require.Equal(t, "99", capacity[0].Used)
	require.Equal(t, "1", *capacity[0].Remaining)
	require.Equal(t, time.Now().UTC().Format("2006-01-02"), capacity[0].Day.Format("2006-01-02"))
	// Race many retries of one identity: a single additional reservation.
	_, err = d.PutKeyPolicy(ctx, model.KeyPolicy{APIKeyID: key, Rules: []model.SpendingRule{{Currency: "USD", Precision: 100, DailyLimit: "1000"}}})
	require.NoError(t, err)
	ids := make(chan string, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, e := d.AdmitPolicy(ctx, key, "retry", []model.PolicyLeg{policyTestLeg(key+"retry", "10")})
			if e != nil {
				t.Errorf("retry admission: %v", e)
				return
			}
			ids <- a.ID
		}()
	}
	wg.Wait()
	close(ids)
	var first string
	for id := range ids {
		if first == "" {
			first = id
		}
		require.Equal(t, first, id)
	}
	capacity, err = d.PolicyCapacity(ctx, key)
	require.NoError(t, err)
	require.Equal(t, "109", capacity[0].Used)
	_, err = d.AdmitPolicy(ctx, key, "retry", []model.PolicyLeg{policyTestLeg(key+"retry", "11")})
	require.ErrorIs(t, err, policy.ErrIdentityConflict)
}

func isPolicyDenied(err error) bool { return errors.Is(err, policy.ErrDenied) }
func TestPolicyPostgresAggregateAllowlistsAndExactAmounts(t *testing.T) {
	d, key := policyTestStore(t)
	ctx := context.Background()
	p := model.KeyPolicy{APIKeyID: key, Rules: []model.SpendingRule{{Currency: "USD", Precision: 100, MaxTransaction: "10", DailyLimit: "100"}}, BalanceIDs: []string{"source", "destination"}, LedgerIDs: []string{"ledger"}}
	_, err := d.PutKeyPolicy(ctx, p)
	require.NoError(t, err)
	_, err = d.AdmitPolicy(ctx, key, "aggregate", []model.PolicyLeg{policyTestLeg(key+"a", "6"), policyTestLeg(key+"b", "6")})
	require.ErrorIs(t, err, policy.ErrDenied)
	forbidden := policyTestLeg(key+"forbidden", "1")
	forbidden.Destination = "outside"
	_, err = d.AdmitPolicy(ctx, key, "forbidden", []model.PolicyLeg{forbidden})
	require.ErrorIs(t, err, policy.ErrDenied)
	forbidden = policyTestLeg(key+"precision", "1")
	forbidden.Precision = 1
	_, err = d.AdmitPolicy(ctx, key, "precision", []model.PolicyLeg{forbidden})
	require.ErrorIs(t, err, policy.ErrDenied)
	p.Rules[0].MaxTransaction = "9007199254740993"
	p.Rules[0].DailyLimit = "9007199254740993"
	_, err = d.PutKeyPolicy(ctx, p)
	require.NoError(t, err)
	_, err = d.AdmitPolicy(ctx, key, "exact", []model.PolicyLeg{policyTestLeg(key+"exact", "9007199254740993")})
	require.NoError(t, err)
	_, err = d.AdmitPolicy(ctx, key, "extra", []model.PolicyLeg{policyTestLeg(key+"extra", "1")})
	require.ErrorIs(t, err, policy.ErrDenied)
	capacity, err := d.PolicyCapacity(ctx, key)
	require.NoError(t, err)
	require.Equal(t, "9007199254740993", capacity[0].Used)
}

func TestPolicyPostgresDecisionAuthorityAndFinality(t *testing.T) {
	d, key := policyTestStore(t)
	ctx := context.Background()
	_, err := d.PutKeyPolicy(ctx, model.KeyPolicy{APIKeyID: key, Rules: []model.SpendingRule{{Currency: "USD", Precision: 100, ApprovalAbove: "10"}}})
	require.NoError(t, err)
	leg := policyTestLeg(key+"hold", "11")
	a, err := d.AdmitPolicy(ctx, key, "hold", []model.PolicyLeg{leg})
	require.NoError(t, err)
	require.Equal(t, "pending", a.Decision)
	require.ErrorIs(t, d.CheckPolicyCommit(ctx, leg.TransactionID), policy.ErrApprovalRequired)
	_, err = d.DecidePolicyApproval(ctx, a.ID, policy.Principal{APIKeyID: key, Scopes: []string{"approvals:write"}}, true)
	require.ErrorIs(t, err, policy.ErrDenied)
	_, err = d.DecidePolicyApproval(ctx, a.ID, policy.Principal{APIKeyID: "other", Scopes: []string{"*:*"}}, true)
	require.ErrorIs(t, err, policy.ErrDenied)
	a, err = d.DecidePolicyApproval(ctx, a.ID, policy.Principal{Master: true}, false)
	require.NoError(t, err)
	require.Equal(t, "rejected", a.Decision)
	_, err = d.DecidePolicyApproval(ctx, a.ID, policy.Principal{Master: true}, true)
	require.Error(t, err)
	require.ErrorIs(t, d.CheckPolicyCommit(ctx, leg.TransactionID), policy.ErrApprovalRequired)
	// Removing a policy does not remove its outstanding approval boundary.
	require.NoError(t, d.DeleteKeyPolicy(ctx, key))
	require.ErrorIs(t, d.CheckPolicyCommit(ctx, leg.TransactionID), policy.ErrApprovalRequired)
	p, err := d.GetKeyPolicy(ctx, key)
	require.NoError(t, err)
	require.Nil(t, p)
}

func TestPolicyDelegatedSharedBudgetsAndRetriesPostgres(t *testing.T) {
	d := openRealTestDB(t)
	ctx := context.Background()
	scopes := []string{"api-keys:write", "transactions:write", "approvals:write"}
	root, err := d.CreateAPIKey(ctx, "root", model.GenerateUUIDWithSuffix("owner"), scopes, time.Now().Add(time.Hour))
	require.NoError(t, err)
	rootCtx := policy.WithPrincipal(ctx, policy.Principal{APIKeyID: root.APIKeyID, Scopes: scopes})
	child, err := d.CreateAPIKey(rootCtx, "child", root.OwnerID, scopes, root.ExpiresAt)
	require.NoError(t, err)
	childCtx := policy.WithPrincipal(ctx, policy.Principal{APIKeyID: child.APIKeyID, Scopes: scopes})
	grandchild, err := d.CreateAPIKey(childCtx, "grandchild", root.OwnerID, scopes, root.ExpiresAt)
	require.NoError(t, err)
	_, err = d.PutKeyPolicy(ctx, model.KeyPolicy{APIKeyID: root.APIKeyID, Rules: []model.SpendingRule{{Currency: "USD", Precision: 100, MaxTransaction: "50", DailyLimit: "100", ApprovalAbove: "40"}}})
	require.NoError(t, err)
	_, err = d.AdmitPolicy(ctx, grandchild.APIKeyID, "denied", []model.PolicyLeg{policyTestLeg(model.GenerateUUIDWithSuffix("ref"), "60")})
	require.ErrorIs(t, err, policy.ErrDenied)
	narrow := model.KeyPolicy{APIKeyID: child.APIKeyID, Rules: []model.SpendingRule{{Currency: "USD", Precision: 100, MaxTransaction: "20", DailyLimit: "100", ApprovalAbove: "10"}}}
	_, err = d.PutKeyPolicy(ctx, narrow)
	require.NoError(t, err)
	narrow.Rules[0].MaxTransaction = "51"
	_, err = d.PutKeyPolicy(ctx, narrow)
	require.ErrorIs(t, err, policy.ErrDenied)
	inherited, err := d.GetKeyPolicy(ctx, grandchild.APIKeyID)
	require.NoError(t, err)
	require.Equal(t, "20", inherited.Rules[0].MaxTransaction)
	require.Len(t, inherited.InheritedFrom, 2)
	firstLeg := policyTestLeg(model.GenerateUUIDWithSuffix("ref"), "11")
	first, err := d.AdmitPolicy(ctx, grandchild.APIKeyID, "first", []model.PolicyLeg{firstLeg})
	require.NoError(t, err)
	require.Equal(t, "pending", first.Decision)
	var accepted atomic.Int32
	start := make(chan struct{})
	failures := make(chan error, 40)
	var wg sync.WaitGroup
	ids := []string{root.APIKeyID, child.APIKeyID, grandchild.APIKeyID}
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			ref := model.GenerateUUIDWithSuffix("delegated_ref")
			_, err := d.AdmitPolicy(ctx, ids[i%len(ids)], ref, []model.PolicyLeg{policyTestLeg(ref, "11")})
			if err == nil {
				accepted.Add(1)
			} else if !isPolicyDenied(err) {
				failures <- err
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	require.Equal(t, int32(8), accepted.Load())
	retryIDs := make(chan string, 20)
	retryErrors := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			retry, err := d.AdmitPolicy(ctx, grandchild.APIKeyID, "first", []model.PolicyLeg{firstLeg})
			if err != nil {
				retryErrors <- err
			} else {
				retryIDs <- retry.ID
			}
		}()
	}
	wg.Wait()
	close(retryIDs)
	close(retryErrors)
	for err := range retryErrors {
		require.NoError(t, err)
	}
	require.Len(t, retryIDs, 20)
	for id := range retryIDs {
		require.Equal(t, first.ID, id)
	}
	for _, id := range ids {
		capacity, err := d.PolicyCapacity(ctx, id)
		require.NoError(t, err)
		require.Equal(t, "1", *capacity[0].Remaining)
		var shared string
		for _, budget := range capacity[0].Budgets {
			if budget.APIKeyID == root.APIKeyID {
				shared = budget.Used
			}
		}
		require.Equal(t, "99", shared)
	}
	// Neither deletion nor a new child resets inherited capacity.
	require.NoError(t, d.DeleteKeyPolicy(ctx, child.APIKeyID))
	_, err = d.AdmitPolicy(ctx, grandchild.APIKeyID, "no-reset", []model.PolicyLeg{policyTestLeg(model.GenerateUUIDWithSuffix("ref"), "2")})
	require.ErrorIs(t, err, policy.ErrDenied)
	// Reparenting would evade both shared budgets and independent review.
	_, err = d.Conn.ExecContext(ctx, `UPDATE ledgerforge.api_keys SET parent_api_key_id=NULL WHERE api_key_id=$1`, child.APIKeyID)
	require.Error(t, err)
}
