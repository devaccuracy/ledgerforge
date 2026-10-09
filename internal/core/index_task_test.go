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
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/devaccuracy/ledgerforge/database/mocks"
	"github.com/devaccuracy/ledgerforge/internal/search"
	"github.com/devaccuracy/ledgerforge/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestProcessIndexRefresh_LedgerUsesLatestDatabaseStateRegardlessOfTaskOrder(t *testing.T) {
	mockDS := new(mocks.MockDataSource)
	b := &LedgerForge{datasource: mockDS}
	ctx := context.Background()

	stateA := &model.Ledger{
		LedgerID: "ldg_order",
		Name:     "Main",
		MetaData: map[string]interface{}{"step": "a"},
	}
	stateB := &model.Ledger{
		LedgerID: "ldg_order",
		Name:     "Main",
		MetaData: map[string]interface{}{"step": "b"},
	}

	mockDS.On("GetLedgerByID", "ldg_order").Return(stateA, nil).Once()
	mockDS.On("GetLedgerByID", "ldg_order").Return(stateB, nil).Once()

	var indexed []map[string]interface{}
	upsert := func(_ context.Context, collection string, document interface{}) error {
		require.Equal(t, "ledgers", collection)
		doc, err := DocumentToIndexMap(document)
		require.NoError(t, err)
		indexed = append(indexed, doc)
		return nil
	}

	task := IndexTask{Collection: "ledgers", DocumentID: "ldg_order", Mode: IndexModeRefresh}
	require.NoError(t, b.ProcessIndexTask(ctx, task, upsert))
	require.NoError(t, b.ProcessIndexTask(ctx, task, upsert))

	require.Len(t, indexed, 2)
	assert.Equal(t, "a", indexed[0]["meta_data"].(map[string]interface{})["step"])
	assert.Equal(t, "b", indexed[1]["meta_data"].(map[string]interface{})["step"],
		"second refresh must reflect the latest DB read even if tasks were enqueued out of commit order")
	mockDS.AssertExpectations(t)
}

func TestProcessIndexRefresh_TransactionScopeHydratesFlags(t *testing.T) {
	mockDS := new(mocks.MockDataSource)
	b := &LedgerForge{datasource: mockDS}
	ctx := context.Background()

	effectiveDate := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	txn := &model.Transaction{
		Inflight: true, AllowOverdraft: true,
		TransactionID: "txn_scope",
		Amount:        100,
		Currency:      "USD",
		Status:        "INFLIGHT",
		EffectiveDate: &effectiveDate,
		MetaData: map[string]interface{}{
			"inflight":        true,
			"allow_overdraft": true,
		},
	}
	mockDS.On("ListTransactionsByMetadataScope", mock.Anything, "txn_scope", 100, int64(0)).
		Return([]*model.Transaction{txn}, nil).Once()

	var indexed map[string]interface{}
	err := b.ProcessIndexTask(ctx, IndexTask{
		Collection: "transactions",
		ScopeID:    "txn_scope",
		Mode:       IndexModeRefresh,
	}, func(_ context.Context, collection string, document interface{}) error {
		require.Equal(t, "transactions", collection)
		raw, err := json.Marshal(document)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(raw, &indexed))
		return nil
	})
	require.NoError(t, err)

	normalized := search.NormalizeTransactionDocument(indexed)
	assert.Equal(t, true, normalized["inflight"])
	assert.Equal(t, true, normalized["allow_overdraft"])
	assert.Equal(t, effectiveDate.Unix(), normalized["effective_date"])
	mockDS.AssertExpectations(t)
}

func TestProcessIndexTask_LegacySnapshotPathUnchanged(t *testing.T) {
	b := &LedgerForge{}
	called := false
	err := b.ProcessIndexTask(context.Background(), IndexTask{
		Collection: "ledgers",
		Payload:    map[string]interface{}{"ledger_id": "ldg_legacy", "name": "Legacy"},
	}, func(_ context.Context, collection string, document interface{}) error {
		called = true
		assert.Equal(t, "ledgers", collection)
		doc, ok := document.(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "ldg_legacy", doc["ledger_id"])
		return nil
	})
	require.NoError(t, err)
	assert.True(t, called)
}

func TestProcessIndexRefresh_BalanceUsesWorkerTimeRead(t *testing.T) {
	mockDS := new(mocks.MockDataSource)
	b := &LedgerForge{datasource: mockDS}

	postUpdate := &model.Balance{
		BalanceID:     "bln_idx_worker",
		Currency:      "USD",
		Balance:       big.NewInt(500),
		CreditBalance: big.NewInt(500),
		MetaData:      map[string]interface{}{"tier": "gold"},
	}
	mockDS.On("GetBalanceByID", "bln_idx_worker", mock.Anything, false).Return(postUpdate, nil).Once()

	var indexed map[string]interface{}
	err := b.ProcessIndexTask(context.Background(), IndexTask{
		Collection: "balances",
		DocumentID: "bln_idx_worker",
		Mode:       IndexModeRefresh,
	}, func(_ context.Context, collection string, document interface{}) error {
		doc, err := DocumentToIndexMap(document)
		require.NoError(t, err)
		indexed = doc
		return nil
	})
	require.NoError(t, err)
	assert.EqualValues(t, 500, indexed["balance"])
	mockDS.AssertExpectations(t)
}
