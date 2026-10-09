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

package mcpserver

import (
	"context"
	"testing"
	"time"

	"github.com/devaccuracy/ledgerforge/internal/policy"
	"github.com/devaccuracy/ledgerforge/model"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

type policyFakeService struct {
	fakeService
	key       model.APIKey
	principal policy.Principal
}

func (s *policyFakeService) GetAPIKeyByKey(_ context.Context, raw string) (*model.APIKey, error) {
	if raw != "configured-secret" {
		return nil, policy.ErrDenied
	}
	copy := s.key
	return &copy, nil
}

func (s *policyFakeService) QueueTransaction(ctx context.Context, t *model.Transaction) (*model.Transaction, error) {
	s.principal = policy.FromContext(ctx)
	return s.fakeService.QueueTransaction(ctx, t)
}

func (s *policyFakeService) GetKeyPolicy(ctx context.Context, key string) (*model.KeyPolicy, error) {
	s.principal = policy.FromContext(ctx)
	return &model.KeyPolicy{APIKeyID: key, Version: 1, Rules: []model.SpendingRule{{Currency: "USD", Precision: 100, DailyLimit: "100"}}}, nil
}

func (s *policyFakeService) PolicyCapacity(context.Context, string) ([]model.PolicyCapacity, error) {
	remaining := "100"
	return []model.PolicyCapacity{{Currency: "USD", Precision: 100, Used: "0", Remaining: &remaining}}, nil
}

func (s *policyFakeService) ListPolicyApprovals(ctx context.Context) ([]model.PolicyAdmission, error) {
	s.principal = policy.FromContext(ctx)
	return []model.PolicyAdmission{}, nil
}

func TestPolicyMCPIdentityScopesRevocationAndResources(t *testing.T) {
	service := &policyFakeService{key: model.APIKey{APIKeyID: "agent-key", ExpiresAt: time.Now().Add(time.Hour), Scopes: []string{"transactions:write"}}}
	session := connect(t, New(service, Options{AllowWrites: true, APIKey: "configured-secret"}))
	tools, err := session.ListTools(context.Background(), nil)
	require.NoError(t, err)
	names := toolNames(tools.Tools)
	require.Contains(t, names, "ledgerforge_effective_policy")
	require.Contains(t, names, "ledgerforge_pending_approvals")
	require.Contains(t, names, "ledgerforge_submit_policy_transaction")
	require.NotContains(t, names, "ledgerforge_approve_transaction")
	require.NotContains(t, names, "ledgerforge_approve_policy_transaction")
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "ledgerforge_submit_policy_transaction", Arguments: map[string]any{"source_balance_id": "source", "destination_balance_id": "destination", "amount_minor": "9007199254740993", "currency": "USD", "currency_multiplier": 100, "reference": "exact", "confirm": true, "metadata": map[string]any{"LEDGERFORGE_GENERATED_BY": "master"}}})
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Equal(t, "agent-key", service.principal.APIKeyID)
	require.False(t, service.principal.Master)
	require.Equal(t, "9007199254740993", service.queuedTransaction.PreciseAmount.String())
	result, err = session.CallTool(context.Background(), &mcp.CallToolParams{Name: "ledgerforge_create_ledger", Arguments: map[string]any{"name": "forbidden", "confirm": true}})
	require.NoError(t, err)
	require.True(t, result.IsError)
	require.Empty(t, service.createdLedger.Name)
	resource, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "ledgerforge://policy/effective"})
	require.NoError(t, err)
	require.Contains(t, resource.Contents[0].Text, "remaining_minor")
	service.key.IsRevoked = true
	result, err = session.CallTool(context.Background(), &mcp.CallToolParams{Name: "ledgerforge_effective_policy", Arguments: map[string]any{}})
	require.NoError(t, err)
	require.True(t, result.IsError)
}
