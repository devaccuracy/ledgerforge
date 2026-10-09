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
	"math"
	"math/big"
	"testing"

	"github.com/devaccuracy/ledgerforge/model"
	"github.com/stretchr/testify/require"
)

func TestJSONAmountRetainsExactDecimal(t *testing.T) {
	const token = "1.004999999999999999999"
	var create RecordTransaction
	require.NoError(t, json.Unmarshal([]byte(`{"amount":`+token+`,"precision":100}`), &create))
	minor, err := model.ApplyPrecisionChecked(create.ToTransaction())
	require.NoError(t, err)
	require.Equal(t, "100", minor.String())
	var single InflightUpdate
	require.NoError(t, json.Unmarshal([]byte(`{"amount":`+token+`}`), &single))
	require.Equal(t, token, single.ExactAmount())
	var bulk BulkInflightCommitRequest
	require.NoError(t, json.Unmarshal([]byte(`{"transactions":[{"transaction_id":"txn","amount":`+token+`}]}`), &bulk))
	require.Equal(t, token, bulk.Transactions[0].ExactAmount())
	// Struct reuse replaces decimal tokens and preserves omitted fields.
	require.NoError(t, json.Unmarshal([]byte(`{"amount":2}`), &create))
	require.Equal(t, "2", create.ExactAmount())
	require.NoError(t, json.Unmarshal([]byte(`{}`), &create))
	require.Equal(t, "2", create.ExactAmount())
	create.Amount = 3
	require.Equal(t, "3", create.ExactAmount())
}

func TestAPIMonetaryValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*RecordTransaction)
	}{
		{"NaN amount", func(tx *RecordTransaction) { tx.Amount = math.NaN() }},
		{"infinite amount", func(tx *RecordTransaction) { tx.Amount = math.Inf(1) }},
		{"NaN precision", func(tx *RecordTransaction) { tx.Precision = math.NaN() }},
		{"infinite precision", func(tx *RecordTransaction) { tx.Precision = math.Inf(1) }},
		{"negative rate", func(tx *RecordTransaction) { tx.Rate = -1 }},
		{"infinite rate", func(tx *RecordTransaction) { tx.Rate = math.Inf(1) }},
		{"NaN rate", func(tx *RecordTransaction) { tx.Rate = math.NaN() }},
		{"negative overdraft", func(tx *RecordTransaction) { tx.OverdraftLimit = -1 }},
		{"infinite overdraft", func(tx *RecordTransaction) { tx.OverdraftLimit = math.Inf(1) }},
		{"NaN overdraft", func(tx *RecordTransaction) { tx.OverdraftLimit = math.NaN() }},
		{"rounds to zero", func(tx *RecordTransaction) { tx.Amount = 0.0001 }},
		{"negative precise", func(tx *RecordTransaction) { tx.Amount = 0; tx.PreciseAmount = big.NewInt(-1) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := &RecordTransaction{Amount: 1, Precision: 100, Currency: "USD", Reference: "reference", Description: "test", Source: "source", Destination: "destination"}
			tc.edit(tx)
			require.Error(t, tx.ValidateRecordTransaction())
		})
	}
	tx := &RecordTransaction{Amount: 1, Currency: "USD", Reference: "reference", Description: "test", Source: "source", Destination: "destination"}
	require.NoError(t, tx.ValidateRecordTransaction())
}
