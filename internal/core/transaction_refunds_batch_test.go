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
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/devaccuracy/ledgerforge/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReconcileRefundBatchResults(t *testing.T) {
	applied := []*model.Transaction{{TransactionID: "txn_refund_b"}}

	t.Run("no error", func(t *testing.T) {
		got, err := reconcileRefundBatchResults(applied, nil)
		require.NoError(t, err)
		assert.Equal(t, applied, got)
	})

	t.Run("applied with skippable already-refunded error", func(t *testing.T) {
		batchErr := &errTransactionAlreadyRefunded{transactionID: "txn_leg_a"}
		got, err := reconcileRefundBatchResults(applied, batchErr)
		require.NoError(t, err)
		assert.Equal(t, applied, got)
	})

	t.Run("applied with joined already-refunded errors", func(t *testing.T) {
		batchErr := errors.Join(
			&errTransactionAlreadyRefunded{transactionID: "txn_leg_a"},
			&errTransactionAlreadyRefunded{transactionID: "txn_leg_c"},
		)
		got, err := reconcileRefundBatchResults(applied, batchErr)
		require.NoError(t, err)
		assert.Equal(t, applied, got)
	})

	t.Run("nothing applied all already refunded", func(t *testing.T) {
		batchErr := errors.Join(
			&errTransactionAlreadyRefunded{transactionID: "txn_leg_a"},
			&errTransactionAlreadyRefunded{transactionID: "txn_leg_b"},
		)
		got, err := reconcileRefundBatchResults(nil, batchErr)
		require.Error(t, err)
		assert.Nil(t, got)
		assert.True(t, allRefundBatchErrorsAreAlreadyRefunded(err))
	})

	t.Run("applied with fatal error", func(t *testing.T) {
		fatal := fmt.Errorf("failed to queue refund transaction")
		got, err := reconcileRefundBatchResults(applied, fatal)
		require.Error(t, err)
		assert.Equal(t, applied, got)
		assert.Equal(t, fatal, err)
	})

	t.Run("applied with mixed already-refunded and fatal errors", func(t *testing.T) {
		batchErr := errors.Join(
			&errTransactionAlreadyRefunded{transactionID: "txn_leg_a"},
			fmt.Errorf("failed to queue refund transaction"),
		)
		got, err := reconcileRefundBatchResults(applied, batchErr)
		require.Error(t, err)
		assert.Equal(t, applied, got)
		assert.False(t, isTransactionAlreadyRefundedError(err))
		assert.NotContains(t, err.Error(), "has already been refunded")
		assert.Contains(t, err.Error(), "failed to queue refund transaction")
	})

	t.Run("mixed errors without apply keeps skippable-only conflict", func(t *testing.T) {
		batchErr := errors.Join(
			&errTransactionAlreadyRefunded{transactionID: "txn_leg_a"},
			fmt.Errorf("failed to queue refund transaction"),
		)
		got, err := reconcileRefundBatchResults(nil, batchErr)
		require.Error(t, err)
		assert.Nil(t, got)
		assert.Contains(t, err.Error(), "failed to queue refund transaction")
		assert.NotContains(t, err.Error(), "has already been refunded")
	})
}

func TestNonSkippableRefundBatchErrors(t *testing.T) {
	t.Run("joined mixed strips skippable phrase", func(t *testing.T) {
		err := nonSkippableRefundBatchErrors(errors.Join(
			&errTransactionAlreadyRefunded{transactionID: "txn_leg_a"},
			fmt.Errorf("failed to queue refund transaction"),
		))
		require.Error(t, err)
		assert.False(t, strings.Contains(err.Error(), "has already been refunded"))
	})
}
