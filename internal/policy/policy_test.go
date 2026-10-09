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
	"context"
	"math"
	"testing"

	"github.com/devaccuracy/ledgerforge/model"
	"github.com/stretchr/testify/require"
)

func TestPolicyValidation(t *testing.T) {
	valid := model.KeyPolicy{Rules: []model.SpendingRule{{Currency: "USD", Precision: 100, MaxTransaction: "9007199254740993", DailyLimit: "0", ApprovalAbove: "10"}}}
	require.NoError(t, Validate(valid))
	for _, bad := range []string{"-1", "1.5", "01", "1e3", "+1", " 1"} {
		p := valid
		p.Rules = append([]model.SpendingRule(nil), valid.Rules...)
		p.Rules[0].DailyLimit = bad
		require.Error(t, Validate(p))
	}
	for _, precision := range []float64{0, 0.5, math.NaN(), math.Inf(1)} {
		p := valid
		p.Rules = append([]model.SpendingRule(nil), valid.Rules...)
		p.Rules[0].Precision = precision
		require.Error(t, Validate(p))
	}
}

func TestApprovalRequiresExplicitIndependentAuthority(t *testing.T) {
	require.False(t, Principal{APIKeyID: "agent", Scopes: []string{"*:*", "transactions:write", "approvals:*"}}.CanApprove())
	require.True(t, Principal{APIKeyID: "reviewer", Scopes: []string{"approvals:write"}}.CanApprove())
	require.True(t, Principal{Master: true}.CanApprove())
	scopes := []string{"transactions:write"}
	ctx := WithPrincipal(context.Background(), Principal{APIKeyID: "trusted", Scopes: scopes})
	scopes[0] = "approvals:write"
	require.False(t, FromContext(ctx).CanApprove())
	require.Equal(t, "trusted", FromContext(ctx).APIKeyID)
}
