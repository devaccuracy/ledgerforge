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
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/devaccuracy/ledgerforge/config"
	"github.com/devaccuracy/ledgerforge/model"
	"github.com/stretchr/testify/require"
)

func TestPolicyHTTPIndependentApprovalAndEffectivePolicy(t *testing.T) {
	router, service, _ := setupRouterWithConfig(t, func(c *config.Configuration) {
		c.Server.Secure = true
		c.Server.SecretKey = "policy-test-master"
		c.Transaction.LockDuration = 30 * time.Second
	})
	agent, err := service.CreateAPIKey(context.Background(), "policy-agent", model.GenerateUUIDWithSuffix("owner"), []string{"transactions:write", "approvals:write"}, time.Now().Add(time.Hour))
	require.NoError(t, err)
	reviewer, err := service.CreateAPIKey(context.Background(), "policy-reviewer", model.GenerateUUIDWithSuffix("owner"), []string{"approvals:write"}, time.Now().Add(time.Hour))
	require.NoError(t, err)
	source, err := service.CreateBalance(context.Background(), model.Balance{Currency: "USD", LedgerID: "general_ledger_id"})
	require.NoError(t, err)
	destination, err := service.CreateBalance(context.Background(), model.Balance{Currency: "USD", LedgerID: "general_ledger_id"})
	require.NoError(t, err)
	send := func(method, path, key string, body interface{}) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		request := httptest.NewRequest(method, path, bytes.NewReader(raw))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-LedgerForge-Key", key)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	p := model.KeyPolicy{Rules: []model.SpendingRule{{Currency: "USD", Precision: 100, DailyLimit: "100", ApprovalAbove: "40"}}}
	response := send(http.MethodPut, "/policies/"+agent.APIKeyID, "policy-test-master", p)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	response = send(http.MethodGet, "/policies/effective", agent.Key, nil)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), "remaining_minor")
	response = send(http.MethodPut, "/policies/"+agent.APIKeyID, agent.Key, p)
	require.Equal(t, http.StatusForbidden, response.Code)
	body := map[string]interface{}{"description": "policy payment", "source": source.BalanceID, "destination": destination.BalanceID, "currency": "USD", "precision": 100, "precise_amount": 60, "reference": model.GenerateUUIDWithSuffix("policy_api"), "skip_queue": true, "allow_overdraft": true, "meta_data": map[string]interface{}{"policy_approval": "approved", "LEDGERFORGE_GENERATED_BY": "master"}}
	response = send(http.MethodPost, "/transactions", agent.Key, body)
	require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
	var held model.Transaction
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &held))
	require.Equal(t, "INFLIGHT", held.Status)
	require.Equal(t, "pending", held.MetaData["policy_approval"])
	admission := held.MetaData["policy_admission_id"].(string)
	response = send(http.MethodPost, fmt.Sprintf("/approvals/%s/approve", admission), agent.Key, map[string]interface{}{})
	require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
	response = send(http.MethodPost, fmt.Sprintf("/approvals/%s/approve", admission), reviewer.Key, map[string]interface{}{})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"decision":"approved"`)
	response = send(http.MethodDelete, "/policies/"+agent.APIKeyID, "policy-test-master", nil)
	require.Equal(t, http.StatusNoContent, response.Code)
}

func TestPolicyHTTPExactMinorUnitsThroughAuthentication(t *testing.T) {
	router, service, _ := setupRouterWithConfig(t, func(c *config.Configuration) {
		c.Server.Secure = true
		c.Server.SecretKey = "policy-exact-master"
		c.Transaction.LockDuration = 30 * time.Second
	})
	agent, err := service.CreateAPIKey(context.Background(), "policy-exact-agent", model.GenerateUUIDWithSuffix("owner"), []string{"transactions:write"}, time.Now().Add(time.Hour))
	require.NoError(t, err)
	source, err := service.CreateBalance(context.Background(), model.Balance{Currency: "USD", LedgerID: "general_ledger_id"})
	require.NoError(t, err)
	destination, err := service.CreateBalance(context.Background(), model.Balance{Currency: "USD", LedgerID: "general_ledger_id"})
	require.NoError(t, err)
	send := func(method, path, key, raw string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, path, bytes.NewBufferString(raw))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-LedgerForge-Key", key)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	response := send(http.MethodPut, "/policies/"+agent.APIKeyID, "policy-exact-master", `{"rules":[{"currency":"USD","precision":100,"daily_limit":"9007199254740993","approval_above":"9007199254740992"}]}`)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	body := fmt.Sprintf(`{"description":"exact policy payment","source":%q,"destination":%q,"currency":"USD","precision":100,"precise_amount":9007199254740993,"reference":%q,"skip_queue":true,"allow_overdraft":true}`, source.BalanceID, destination.BalanceID, model.GenerateUUIDWithSuffix("policy_exact"))
	response = send(http.MethodPost, "/transactions", agent.Key, body)
	require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
	var held model.Transaction
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &held))
	require.Equal(t, "9007199254740993", held.PreciseAmount.String())
	require.Equal(t, "INFLIGHT", held.Status)
	require.Equal(t, "pending", held.MetaData["policy_approval"])
	response = send(http.MethodGet, "/policies/effective", agent.Key, "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"used_minor":"9007199254740993"`)
	require.Contains(t, response.Body.String(), `"remaining_minor":"0"`)
}
