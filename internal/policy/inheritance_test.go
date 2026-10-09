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

package policy

import (
	"github.com/devaccuracy/ledgerforge/model"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPolicyIntersectionAndNarrowing(t *testing.T) {
	parent := model.KeyPolicy{APIKeyID: "parent", Rules: []model.SpendingRule{{Currency: "USD", Precision: 100, MaxTransaction: "50", DailyLimit: "100", ApprovalAbove: "40"}}, BalanceIDs: []string{"a", "b"}, LedgerIDs: []string{"ledger"}}
	child := model.KeyPolicy{APIKeyID: "child", Rules: []model.SpendingRule{{Currency: "USD", Precision: 100, MaxTransaction: "20", DailyLimit: "80", ApprovalAbove: "10"}}, BalanceIDs: []string{"a"}, LedgerIDs: []string{"ledger"}}
	require.NoError(t, ValidateNarrowing(child, &parent))
	effective := Intersect("child", []model.KeyPolicy{child, parent})
	require.Equal(t, child.Rules, effective.Rules)
	require.Equal(t, []string{"parent"}, effective.InheritedFrom)
	for _, change := range []func(*model.KeyPolicy){
		func(p *model.KeyPolicy) { p.Rules[0].MaxTransaction = "51" },
		func(p *model.KeyPolicy) { p.Rules[0].DailyLimit = "101" },
		func(p *model.KeyPolicy) { p.Rules[0].ApprovalAbove = "41" },
		func(p *model.KeyPolicy) { p.Rules[0].MaxTransaction = "" },
		func(p *model.KeyPolicy) { p.Rules[0].Currency = "EUR" },
		func(p *model.KeyPolicy) { p.Rules[0].Precision = 1000 },
		func(p *model.KeyPolicy) { p.BalanceIDs = nil },
		func(p *model.KeyPolicy) { p.LedgerIDs = []string{"other"} },
	} {
		p := child
		p.Rules = append([]model.SpendingRule(nil), child.Rules...)
		change(&p)
		require.ErrorIs(t, ValidateNarrowing(p, &parent), ErrDenied)
	}
	disjoint := child
	disjoint.BalanceIDs = []string{"other"}
	require.True(t, Intersect("child", []model.KeyPolicy{parent, disjoint}).DenyAll)
	disjoint = child
	disjoint.Rules = []model.SpendingRule{{Currency: "EUR", Precision: 100}}
	require.True(t, Intersect("child", []model.KeyPolicy{parent, disjoint}).DenyAll)
	// Tightening an ancestor later takes effect without rewriting child policies.
	parent.Rules[0].MaxTransaction = "5"
	require.Equal(t, "5", Intersect("child", []model.KeyPolicy{parent, child}).Rules[0].MaxTransaction)
}
