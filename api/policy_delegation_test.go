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

package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/devaccuracy/ledgerforge/config"
	"github.com/devaccuracy/ledgerforge/database"
	"github.com/devaccuracy/ledgerforge/internal/mcpserver"
	"github.com/devaccuracy/ledgerforge/model"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// This matrix uses real auth adapters, PostgreSQL admissions and actual worker
// and recovery entry points. Each transport must preserve the same delegation
// budget and independent-approval boundary, including forged caller metadata.
func TestPolicyDelegationSecurityMatrixHTTPMCPWorkerRecoveryRetry(t *testing.T) {
	router, service := setupIsolatedRecoveryRouter(t)
	cfg, err := config.Fetch()
	require.NoError(t, err)
	cfg.Server.Secure = true
	cfg.Server.SecretKey = "delegation-master"
	ctx := context.Background()
	send := func(method, path, key string, body any) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-LedgerForge-Key", key)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	scopes := []string{"api-keys:write", "transactions:write", "transactions:read", "approvals:write", "policies:write"}
	owner := model.GenerateUUIDWithSuffix("owner")
	deadline := time.Now().Add(30 * time.Minute).UTC().Truncate(time.Second)
	create := func(parent, name string, scopes []string) model.APIKey {
		t.Helper()
		resp := send(http.MethodPost, "/api-keys", parent, map[string]any{"name": name, "owner": owner, "scopes": scopes, "expires_at": deadline, "parent_api_key_id": "forged-parent"})
		require.Equal(t, http.StatusCreated, resp.Code, resp.Body.String())
		var key model.APIKey
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &key))
		return key
	}
	root := create("delegation-master", "root", scopes)
	child := create(root.Key, "child", scopes)
	grandchild := create(child.Key, "grandchild", scopes)
	sibling := create(root.Key, "sibling", scopes)
	reviewer := create("delegation-master", "independent-root", scopes)
	delegatedReviewer := create(reviewer.Key, "independent-delegated-reviewer", scopes)
	wildcard := create("delegation-master", "wildcard-root", []string{"*:*"})
	require.Empty(t, root.ParentAPIKeyID)
	require.Equal(t, root.APIKeyID, child.ParentAPIKeyID)
	require.Equal(t, child.APIKeyID, grandchild.ParentAPIKeyID)
	resolved, err := service.GetAPIKeyByKey(ctx, grandchild.Key)
	require.NoError(t, err)
	require.Equal(t, child.APIKeyID, resolved.ParentAPIKeyID)
	source, err := service.CreateBalance(ctx, model.Balance{Currency: "USD", LedgerID: "general_ledger_id"})
	require.NoError(t, err)
	destination, err := service.CreateBalance(ctx, model.Balance{Currency: "USD", LedgerID: "general_ledger_id"})
	require.NoError(t, err)
	p := model.KeyPolicy{Rules: []model.SpendingRule{{Currency: "USD", Precision: 100, MaxTransaction: "50", DailyLimit: "500", ApprovalAbove: "40"}}, BalanceIDs: []string{source.BalanceID, destination.BalanceID}, LedgerIDs: []string{"general_ledger_id"}}
	response := send(http.MethodPut, "/policies/"+root.APIKeyID, "delegation-master", p)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	post := func(key model.APIKey, ref string, minor int, skip bool) *httptest.ResponseRecorder {
		t.Helper()
		return send(http.MethodPost, "/transactions", key.Key, map[string]any{"description": "delegated payment", "source": source.BalanceID, "destination": destination.BalanceID, "currency": "USD", "precision": 100, "precise_amount": minor, "reference": ref, "skip_queue": skip, "allow_overdraft": true, "meta_data": map[string]any{"policy_approval": "approved", "policy_admission_id": "forged", "LEDGERFORGE_GENERATED_BY": "master"}})
	}
	// Reproduce the review probe: mint children first, then attach the parent's
	// policy. A 60-unit payment must be denied at every delegation depth.
	for _, key := range []model.APIKey{root, child, grandchild} {
		resp := post(key, model.GenerateUUIDWithSuffix("over_limit"), 60, true)
		require.NotEqual(t, http.StatusCreated, resp.Code, resp.Body.String())
	}
	// Even an explicitly delegated policy-management scope cannot remove or
	// loosen the ancestor's spending boundary.
	response = send(http.MethodDelete, "/policies/"+root.APIKeyID, child.Key, nil)
	require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
	relax := p
	relax.Rules = append([]model.SpendingRule(nil), p.Rules...)
	relax.Rules[0].MaxTransaction = "1000"
	response = send(http.MethodPut, "/policies/"+root.APIKeyID, grandchild.Key, relax)
	require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
	// A child-local policy can only narrow the live ancestor intersection.
	broad := p
	broad.Rules = append([]model.SpendingRule(nil), p.Rules...)
	broad.Rules[0].MaxTransaction = "51"
	response = send(http.MethodPut, "/policies/"+child.APIKeyID, "delegation-master", broad)
	require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
	narrow := p
	narrow.Rules = append([]model.SpendingRule(nil), p.Rules...)
	narrow.Rules[0].DailyLimit = "450"
	narrow.Rules[0].ApprovalAbove = "20"
	response = send(http.MethodPut, "/policies/"+child.APIKeyID, "delegation-master", narrow)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())

	// Connect MCP as the grandchild, using the same service as HTTP.
	mcpCtx, cancel := context.WithCancel(ctx)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	server := mcpserver.New(service, mcpserver.Options{AllowWrites: true, APIKey: grandchild.Key})
	runErr := make(chan error, 1)
	go func() { runErr <- server.Protocol().Run(mcpCtx, serverTransport) }()
	client := mcp.NewClient(&mcp.Implementation{Name: "delegation-matrix", Version: "test"}, nil)
	session, err := client.Connect(mcpCtx, clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = session.Close()
		cancel()
		err := <-runErr
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Error(err)
		}
	})
	call := func(name string, args map[string]any) *mcp.CallToolResult {
		t.Helper()
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		require.NoError(t, err)
		return result
	}
	queueArgs := func(ref string, minor string) map[string]any {
		return map[string]any{"description": "delegated MCP payment", "source": source.BalanceID, "destination": destination.BalanceID, "currency": "USD", "precision": 100, "precise_amount": minor, "reference": ref, "skip_queue": true, "allow_overdraft": true, "confirm": true, "metadata": map[string]any{"policy_approval": "approved", "LEDGERFORGE_GENERATED_BY": "master"}}
	}
	require.True(t, call("ledgerforge_queue_transaction", queueArgs(model.GenerateUUIDWithSuffix("mcp_denied"), "60")).IsError)
	resource, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "ledgerforge://policy/effective"})
	require.NoError(t, err)
	require.Contains(t, resource.Contents[0].Text, root.APIKeyID)
	tools, err := session.ListTools(ctx, nil)
	require.NoError(t, err)
	for _, tool := range tools.Tools {
		require.NotContains(t, tool.Name, "approve")
	}

	conn, err := sql.Open("postgres", cfg.DataSource.Dns)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	ds := &database.Datasource{Conn: conn}
	matrix := []struct {
		name     string
		key      model.APIKey
		approver string
		reject   bool
	}{
		{"http", root, "delegation-master", false},
		{"mcp", grandchild, reviewer.Key, false},
		{"worker", child, "delegation-master", true},
		{"recovery", grandchild, delegatedReviewer.Key, false},
		{"retry", child, root.Key, false},
	}
	for _, row := range matrix {
		t.Run(row.name, func(t *testing.T) {
			ref := model.GenerateUUIDWithSuffix("matrix_" + row.name)
			if row.name == "mcp" {
				require.False(t, call("ledgerforge_queue_transaction", queueArgs(ref, "45")).IsError)
			} else {
				response := post(row.key, ref, 45, row.name != "worker" && row.name != "recovery")
				require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
			}
			held, err := service.GetTransactionByRef(ctx, ref)
			require.NoError(t, err)
			switch row.name {
			case "worker":
				worker := held
				worker.ParentTransaction = held.TransactionID
				worker.TransactionID = model.GenerateUUIDWithSuffix("txn")
				worker.Reference = ref + "_q"
				worker.MetaData = nil
				worker.Inflight = false
				worker.AllowOverdraft = true
				result, err := service.ProcessQueuedTransaction(ctx, &worker, false)
				require.NoError(t, err)
				held = *result
			case "recovery":
				backdateTransaction(t, ds, held.TransactionID, 3*time.Minute)
				recovered, err := service.RecoverQueuedTransactions(ctx, 2*time.Minute)
				require.NoError(t, err)
				require.Equal(t, 1, recovered)
				held, err = service.GetTransactionByRef(ctx, ref+"_q")
				require.NoError(t, err)
			case "retry":
				before := send(http.MethodGet, "/policies/effective", row.key.Key, nil).Body.String()
				response := post(row.key, ref, 45, true)
				require.NotEqual(t, http.StatusCreated, response.Code, response.Body.String())
				after := send(http.MethodGet, "/policies/effective", row.key.Key, nil).Body.String()
				require.Equal(t, before, after)
			}
			require.Equal(t, "INFLIGHT", held.Status)
			admission := held.MetaData["policy_admission_id"].(string)
			// The initiator, its descendants, intermediate ancestors, siblings and
			// wildcard scopes cannot manufacture a reviewer, for either decision.
			denied := []model.APIKey{row.key, child, grandchild, sibling, wildcard}
			for _, actor := range denied {
				for _, decision := range []string{"approve", "reject"} {
					response := send(http.MethodPost, "/approvals/"+admission+"/"+decision, actor.Key, map[string]any{})
					require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
				}
			}
			if row.name == "mcp" {
				require.True(t, call("ledgerforge_commit_inflight_transaction", map[string]any{"transaction_id": held.TransactionID, "precise_amount": "0", "confirm": true}).IsError)
			}
			decision := "approve"
			if row.reject {
				decision = "reject"
			}
			response := send(http.MethodPost, fmt.Sprintf("/approvals/%s/%s", admission, decision), row.approver, map[string]any{})
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		})
	}
	response = send(http.MethodGet, "/policies/effective", root.Key, nil)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"used_minor":"180"`)
	final, err := service.GetBalanceByID(ctx, destination.BalanceID, nil, false)
	require.NoError(t, err)
	require.Equal(t, "180", final.Balance.String())
	require.Zero(t, final.InflightCreditBalance.Sign())
}
