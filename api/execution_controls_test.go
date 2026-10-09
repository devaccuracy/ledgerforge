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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/devaccuracy/ledgerforge/config"
	"github.com/devaccuracy/ledgerforge/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMetadataAPIRejectsReservedExecutionKeys(t *testing.T) {
	router := gin.New()
	router.POST("/:entity-id/metadata", (Api{}).UpdateMetadata)
	for _, key := range []string{"allow_overdraft", "overdraft_limit", "inflight", "atomic", "skip_queue", "queue_action", "QUEUED_PARENT_TRANSACTION", "queue_lane", "recovery_attempts", "__execution_controls"} {
		req := httptest.NewRequest(http.MethodPost, "/txn_test/metadata", strings.NewReader(`{"meta_data":{"`+key+`":true}}`))
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		require.Equal(t, http.StatusBadRequest, response.Code)
		require.Contains(t, response.Body.String(), "reserved transaction execution metadata key")
	}
}

func TestMetadataOnlyKeyCannotChangeQueuedExecution(t *testing.T) {
	router, service, _ := setupRouterWithConfig(t, func(cfg *config.Configuration) {
		cfg.Server.Secure = true
		cfg.Server.SecretKey = "integrity-controls-master"
	})
	t.Cleanup(func() { _ = service.Close() })
	key, err := service.CreateAPIKey(context.Background(), "metadata-only", model.GenerateUUIDWithSuffix("owner"), []string{"metadata:write"}, time.Now().Add(time.Hour))
	require.NoError(t, err)
	src, err := service.CreateBalance(t.Context(), model.Balance{Currency: "USD", LedgerID: "general_ledger_id"})
	require.NoError(t, err)
	dst, err := service.CreateBalance(t.Context(), model.Balance{Currency: "USD", LedgerID: "general_ledger_id"})
	require.NoError(t, err)
	intent, err := service.QueueTransaction(t.Context(), &model.Transaction{Source: src.BalanceID, Destination: dst.BalanceID, Amount: 100, Precision: 1, Currency: "USD", Reference: model.GenerateUUIDWithSuffix("ref")})
	require.NoError(t, err)
	send := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/"+intent.TransactionID+"/metadata", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-LedgerForge-Key", key.Key)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	response := send(`{"meta_data":{"allow_overdraft":true}}`)
	require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	response = send(`{"meta_data":{"customer_note":"editable"}}`)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	persisted, err := service.GetTransaction(t.Context(), intent.TransactionID)
	require.NoError(t, err)
	require.False(t, persisted.AllowOverdraft)
	require.Equal(t, "editable", persisted.MetaData["customer_note"])
}
