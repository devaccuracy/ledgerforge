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
	"math/big"
	"testing"
	"time"

	"github.com/devaccuracy/ledgerforge/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecoverZeroAmountQueuedTransactionWritesRejectedChild(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping zero-amount recovery integration test in short mode")
	}

	b, ds := newCoreTestLedgerForge(t)
	src, dst := newBalancePair(t, ds)
	ctx := context.Background()

	parent := &model.Transaction{
		TransactionID: model.GenerateUUIDWithSuffix("txn"),
		Reference:     "starbank_dust_" + model.GenerateUUIDWithSuffix("ref"),
		Source:        src.BalanceID,
		Destination:   dst.BalanceID,
		Amount:        0,
		AmountString:  "0",
		PreciseAmount: big.NewInt(0),
		Precision:     100,
		Currency:      "WBTC",
		Description:   "StarBank shadow mirror (trade)",
		Status:        StatusQueued,
		CreatedAt:     time.Now().UTC().Add(-3 * time.Hour),
		MetaData: map[string]interface{}{
			"recovery_attempts": 3,
		},
	}

	_, err := ds.RecordTransaction(ctx, parent)
	require.NoError(t, err, "parent QUEUED row must persist with amount 0")

	processor := NewQueuedTransactionRecoveryProcessor(b)
	require.NoError(t, processor.processStuckTransaction(ctx, parent))

	children, err := ds.GetTransactionsByParent(ctx, parent.TransactionID, 10, 0)
	require.NoError(t, err)
	require.Len(t, children, 1, "recovery must write a REJECTED child")
	assert.Equal(t, StatusRejected, children[0].Status)
	assert.Equal(t, 0.0, children[0].Amount)
	assert.Equal(t, "0", children[0].PreciseAmount.String())
	assert.Equal(t, parent.TransactionID, children[0].ParentTransaction)

	stuck, err := ds.GetStuckQueuedTransactions(ctx, 2*time.Hour, 100)
	require.NoError(t, err)
	for _, txn := range stuck {
		assert.NotEqual(t, parent.TransactionID, txn.TransactionID, "rejected zero-amount parent must leave the stuck set")
	}
}
