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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToBulkTransactionRequest(t *testing.T) {
	t.Run("Maps all fields and transactions", func(t *testing.T) {
		req := &BulkTransactionRequest{
			Transactions: []*RecordTransaction{
				{
					Amount:      100.5,
					Precision:   100,
					Reference:   "ref_bulk_1",
					Currency:    "USD",
					Source:      "bln_src",
					Destination: "bln_dst",
				},
				{
					Amount:      0.01,
					Precision:   100,
					Reference:   "ref_bulk_2",
					Currency:    "USD",
					Source:      "bln_src",
					Destination: "bln_dst",
				},
			},
			Inflight:  true,
			Atomic:    true,
			RunAsync:  true,
			SkipQueue: true,
		}

		out := req.ToBulkTransactionRequest()
		require.Len(t, out.Transactions, 2)
		assert.True(t, out.Inflight)
		assert.True(t, out.Atomic)
		assert.True(t, out.RunAsync)
		assert.True(t, out.SkipQueue)
		assert.Equal(t, "ref_bulk_1", out.Transactions[0].Reference)
		assert.Equal(t, 100.5, out.Transactions[0].Amount)
		assert.Equal(t, "ref_bulk_2", out.Transactions[1].Reference)
	})

	t.Run("Nil transaction entries stay nil", func(t *testing.T) {
		req := &BulkTransactionRequest{
			Transactions: []*RecordTransaction{nil, {Reference: "ref_x", Amount: 1, Currency: "USD", Source: "a", Destination: "b"}},
		}
		out := req.ToBulkTransactionRequest()
		require.Len(t, out.Transactions, 2)
		assert.Nil(t, out.Transactions[0])
		require.NotNil(t, out.Transactions[1])
		assert.Equal(t, "ref_x", out.Transactions[1].Reference)
	})

	t.Run("Empty request", func(t *testing.T) {
		out := (&BulkTransactionRequest{}).ToBulkTransactionRequest()
		assert.Empty(t, out.Transactions)
		assert.False(t, out.Atomic)
	})
}
