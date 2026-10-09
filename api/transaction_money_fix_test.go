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
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"testing"

	"github.com/devaccuracy/ledgerforge"
	model2 "github.com/devaccuracy/ledgerforge/api/model"
	"github.com/devaccuracy/ledgerforge/internal/apierror"
	redis_db "github.com/devaccuracy/ledgerforge/internal/redisdb"
	"github.com/devaccuracy/ledgerforge/model"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

func TestPreciseAmountSafeMessageAllowlist(t *testing.T) {
	for _, tc := range []struct {
		name, message string
		code          apierror.ErrorCode
		want          string
	}{
		{"exact field message", "precise_amount must be positive", apierror.ErrTxnInvalidAmount, "precise_amount must be positive"},
		{"backend prefix remains hidden", "database password: precise_amount must be positive", apierror.ErrTxnInvalidAmount, "transaction amount must be positive"},
		{"backend suffix remains hidden", "precise_amount must be positive: postgres password", apierror.ErrTxnInvalidAmount, "transaction amount must be positive"},
		{"internal errors remain hidden", "precise_amount must be positive", apierror.ErrGenInternal, "internal server error"},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.want, clientErrorMessage(errors.New(tc.message), tc.code)) })
	}
}

func TestBulkCommitAllInvalidAmountsReturnPerItemResults(t *testing.T) {
	router := newBulkTestRouter()
	response := doJSON(router, http.MethodPost, "/transactions/inflight/bulk/commit", model2.BulkInflightCommitRequest{SkipQueue: true, Transactions: []model2.BulkInflightCommitItem{
		{TransactionID: "txn_a", PreciseAmount: big.NewInt(0)},
		{TransactionID: "txn_b", PreciseAmount: big.NewInt(-1)},
	}})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var result model2.BulkInflightResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	require.Zero(t, result.Succeeded)
	require.Equal(t, 2, result.Failed)
	require.Len(t, result.Results, 2)
	for _, item := range result.Results {
		require.Equal(t, "failed", item.Status)
		require.Equal(t, "INVALID_AMOUNT", item.Code)
		require.Equal(t, "precise_amount must be positive", item.Message)
	}
}

func TestBulkPartialCommitPreservesPerItemOutcomes(t *testing.T) {
	for _, mode := range []struct {
		name              string
		skipQueue, dryRun bool
	}{{"synchronous", true, false}, {"queued", false, false}, {"dry run synchronous", true, true}, {"dry run queued", false, true}} {
		t.Run(mode.name, func(t *testing.T) {
			router, b, err := setupRouter(t)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, b.Close()) })
			source := createTestBalanceWithLedger(t, b, "USD", nil)
			destination := createTestBalanceWithLedger(t, b, "USD", nil)
			first := createInflightForBulk(t, router, source.BalanceID, destination.BalanceID, "USD", 100)
			second := createInflightForBulk(t, router, source.BalanceID, destination.BalanceID, "USD", 100)
			missing := model.GenerateUUIDWithSuffix("txn_missing")
			req := model2.BulkInflightCommitRequest{SkipQueue: mode.skipQueue, DryRun: mode.dryRun, Transactions: []model2.BulkInflightCommitItem{
				{TransactionID: missing, Amount: 5},
				{TransactionID: first, Amount: 40},
				{TransactionID: first, Amount: 0.0001},
				{TransactionID: first, PreciseAmount: big.NewInt(0)},
				{TransactionID: second, Amount: 10},
			}}
			response := doJSON(router, http.MethodPost, "/transactions/inflight/bulk/commit", req)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			if mode.dryRun {
				var preview model.BulkTransactionPreview
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &preview))
				require.Len(t, preview.Results, 5)
				require.False(t, preview.WouldApply)
				require.Equal(t, []bool{false, true, false, false, true}, []bool{preview.Results[0].WouldApply, preview.Results[1].WouldApply, preview.Results[2].WouldApply, preview.Results[3].WouldApply, preview.Results[4].WouldApply})
				require.Equal(t, "4000", preview.Results[1].PreciseAmount)
				require.Equal(t, "1000", preview.Results[4].PreciseAmount)
				require.Equal(t, "GEN_NOT_FOUND", preview.Results[0].Rejection.Code)
				require.Equal(t, "TXN_INVALID_AMOUNT", preview.Results[2].Rejection.Code)
				require.Equal(t, "precise_amount must be positive", preview.Results[3].Rejection.Message)
			} else {
				var result model2.BulkInflightResponse
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
				require.Equal(t, 2, result.Succeeded)
				require.Equal(t, 3, result.Failed)
				require.Len(t, result.Results, 5)
				require.Equal(t, []string{missing, first, first, first, second}, []string{result.Results[0].TransactionID, result.Results[1].TransactionID, result.Results[2].TransactionID, result.Results[3].TransactionID, result.Results[4].TransactionID})
				require.Equal(t, "NOT_FOUND", result.Results[0].Code)
				require.Equal(t, "INVALID_AMOUNT", result.Results[2].Code)
				require.Equal(t, "INVALID_AMOUNT", result.Results[3].Code)
				require.Equal(t, "precise_amount must be positive", result.Results[3].Message)
				wantStatus := "succeeded"
				if !mode.skipQueue {
					wantStatus = "queued"
				}
				require.Equal(t, wantStatus, result.Results[1].Status)
				require.Equal(t, wantStatus, result.Results[4].Status)
			}
			for i, id := range []string{first, second} {
				total, err := b.GetDataSource().GetTotalCommittedTransactions(t.Context(), id)
				require.NoError(t, err)
				want := []int64{4000, 1000}[i]
				if mode.dryRun || !mode.skipQueue {
					want = 0
				}
				if !mode.dryRun && !mode.skipQueue {
					inspector := monetaryTestInspector(t, b)
					task, err := inspector.GetTaskInfo(b.Config().Queue.InflightCommitQueue, "inflight-action:"+id)
					require.NoError(t, err)
					var queued ledgerforge.InflightActionPayload
					require.NoError(t, json.Unmarshal(task.Payload, &queued))
					require.Equal(t, id, queued.TransactionID)
					require.Equal(t, "commit", queued.Action)
					require.Equal(t, big.NewInt([]int64{4000, 1000}[i]).String(), queued.PreciseAmount)
					require.NoError(t, inspector.DeleteTask(b.Config().Queue.InflightCommitQueue, task.ID))
				}
				require.Equal(t, big.NewInt(want), total, "failed conversions must not settle the full remaining hold")
			}
			src, err := b.GetBalanceByID(t.Context(), source.BalanceID, nil, false)
			require.NoError(t, err)
			dst, err := b.GetBalanceByID(t.Context(), destination.BalanceID, nil, false)
			require.NoError(t, err)
			if mode.dryRun || !mode.skipQueue {
				require.Zero(t, src.Balance.Sign())
				require.Zero(t, dst.Balance.Sign())
				require.Equal(t, "20000", src.InflightDebitBalance.String())
				require.Equal(t, "20000", dst.InflightCreditBalance.String())
			} else {
				require.Equal(t, "-5000", src.Balance.String())
				require.Equal(t, "5000", dst.Balance.String())
				require.Equal(t, "15000", src.InflightDebitBalance.String())
				require.Equal(t, "15000", dst.InflightCreditBalance.String())
			}
		})
	}
}

func TestBulkPartialCommitUnderflowIsPerItemFailure(t *testing.T) {
	for _, amountFields := range []string{`"amount":1e-400`, `"amount":1e-400,"amount":null`} {
		t.Run(amountFields, func(t *testing.T) {
			router, b, err := setupRouter(t)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, b.Close()) })
			source := createTestBalanceWithLedger(t, b, "USD", nil)
			destination := createTestBalanceWithLedger(t, b, "USD", nil)
			holdID := createInflightForBulk(t, router, source.BalanceID, destination.BalanceID, "USD", 100)
			body := []byte(fmt.Sprintf(`{"skip_queue":true,"transactions":[{"transaction_id":%q,%s},{"transaction_id":%q,"amount":40}]}`, holdID, amountFields, holdID))
			response := doJSONRequest(router, http.MethodPost, "/transactions/inflight/bulk/commit", body)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			var result model2.BulkInflightResponse
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
			require.Equal(t, 1, result.Succeeded)
			require.Equal(t, 1, result.Failed)
			require.Len(t, result.Results, 2)
			require.Equal(t, "INVALID_AMOUNT", result.Results[0].Code)
			require.Equal(t, "failed", result.Results[0].Status)
			require.Equal(t, "succeeded", result.Results[1].Status)
			total, err := b.GetDataSource().GetTotalCommittedTransactions(t.Context(), holdID)
			require.NoError(t, err)
			require.Equal(t, "4000", total.String(), "underflowing input must not commit the full hold")
		})
	}
}

func monetaryTestInspector(t *testing.T, b *ledgerforge.LedgerForge) *asynq.Inspector {
	t.Helper()
	opt, err := redis_db.NewConnOpt(b.Config().Redis.Dns, b.Config().Redis.SkipTLSVerify)
	require.NoError(t, err)
	inspector := asynq.NewInspector(opt)
	t.Cleanup(func() { require.NoError(t, inspector.Close()) })
	return inspector
}
