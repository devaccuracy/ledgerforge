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

package model

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A rejected projection must not also claim a resulting status. The real
// endpoint returns an error and writes no transaction, so "would_apply": false
// alongside "status": "APPLIED" describes an outcome that cannot happen.
func TestFinalize_RejectedPreviewDropsStatus(t *testing.T) {
	preview := &TransactionPreview{
		DryRun:     true,
		WouldApply: false,
		Status:     "APPLIED",
		Rejection: &PreviewRejection{
			Code:    "TXN_INSUFFICIENT_FUNDS",
			Reason:  "insufficient_funds",
			Message: "failed to apply transaction to balances: insufficient funds in source balance",
		},
	}
	preview.Finalize()

	assert.Empty(t, preview.Status, "a rejected projection has no resulting status")

	raw, err := json.Marshal(preview)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), `"status"`,
		"status must be omitted entirely, not emitted as an empty string: %s", string(raw))
	assert.Contains(t, string(raw), `"would_apply":false`)
	assert.Contains(t, string(raw), `"TXN_INSUFFICIENT_FUNDS"`)
}

// An accepted projection keeps the status it would carry once applied.
func TestFinalize_AcceptedPreviewKeepsStatus(t *testing.T) {
	preview := &TransactionPreview{DryRun: true, WouldApply: true, Status: "APPLIED"}
	preview.Finalize()

	assert.Equal(t, "APPLIED", preview.Status)
	raw, err := json.Marshal(preview)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"status":"APPLIED"`)
}

// Batch finalisation must reach the items, since each carries its own status.
func TestFinalize_BulkClearsStatusPerRejectedItem(t *testing.T) {
	batch := &BulkTransactionPreview{
		DryRun:     true,
		WouldApply: false,
		Results: []TransactionPreview{
			{WouldApply: true, Status: "APPLIED"},
			{WouldApply: false, Status: "APPLIED", Rejection: &PreviewRejection{Code: "TXN_INSUFFICIENT_FUNDS"}},
		},
	}
	batch.Finalize()

	assert.Equal(t, "APPLIED", batch.Results[0].Status, "the accepted item keeps its status")
	assert.Empty(t, batch.Results[1].Status, "the rejected item must not report a status")
}
