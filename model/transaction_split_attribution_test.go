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
	"context"
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func splitFanIn(t *testing.T, sources []Distribution) (*Transaction, []*Transaction) {
	t.Helper()
	txn := &Transaction{
		TransactionID: "txn_parent",
		Reference:     "ref",
		Precision:     100,
		PreciseAmount: big.NewInt(10000),
		Amount:        100,
		Currency:      "USD",
		Destination:   "bln_dst",
		Sources:       sources,
	}
	legs, err := txn.SplitTransactionPrecise(context.Background())
	require.NoError(t, err)
	return txn, legs
}

// Each Distribution entry must carry the id of the leg built for its own
// identifier. The write-back and the reference suffix were both positional
// while the loop ranged over a map, and Go randomises map iteration, so the
// first source regularly received the second source's transaction id. Repeat
// the split so a wrong ordering cannot pass by luck.
func TestSplitTransactionPrecise_AttributesLegsToTheirOwnEntry(t *testing.T) {
	const runs = 200
	for i := 0; i < runs; i++ {
		txn, legs := splitFanIn(t, []Distribution{
			{Identifier: "bln_A", Distribution: "60%"},
			{Identifier: "bln_B", Distribution: "40%"},
		})
		require.Len(t, legs, 2)

		bySource := make(map[string]*Transaction, len(legs))
		for _, leg := range legs {
			bySource[leg.Source] = leg
		}

		for _, d := range txn.Sources {
			leg := bySource[d.Identifier]
			require.NotNil(t, leg, "no leg produced for %s", d.Identifier)
			require.Equal(t, leg.TransactionID, d.TransactionID,
				"entry %s carries the id of a different leg", d.Identifier)
		}
	}
}

// Leg references must be stable: the same split has to map the same identifier
// to the same "-n" suffix on every run, or reconciling by reference attributes
// an amount to the wrong balance.
func TestSplitTransactionPrecise_ReferencesAreStable(t *testing.T) {
	const runs = 200
	for i := 0; i < runs; i++ {
		_, legs := splitFanIn(t, []Distribution{
			{Identifier: "bln_A", Distribution: "60%"},
			{Identifier: "bln_B", Distribution: "40%"},
		})
		require.Len(t, legs, 2)

		byRef := make(map[string]string, len(legs))
		for _, leg := range legs {
			byRef[leg.Reference] = leg.Source
		}
		assert.Equal(t, "bln_A", byRef["ref-1"], "first source must keep the first reference suffix")
		assert.Equal(t, "bln_B", byRef["ref-2"], "second source must keep the second reference suffix")
	}
}

// Legs must be produced in the order the caller supplied them.
func TestSplitTransactionPrecise_PreservesInputOrder(t *testing.T) {
	for i := 0; i < 50; i++ {
		_, legs := splitFanIn(t, []Distribution{
			{Identifier: "bln_A", Distribution: "50%"},
			{Identifier: "bln_B", Distribution: "30%"},
			{Identifier: "bln_C", Distribution: "20%"},
		})
		require.Len(t, legs, 3)
		assert.Equal(t, []string{"bln_A", "bln_B", "bln_C"},
			[]string{legs[0].Source, legs[1].Source, legs[2].Source})
	}
}

func TestSplitTransactionPrecise_RepeatedIdentifierRejected(t *testing.T) {
	txn := &Transaction{Precision: 100, PreciseAmount: big.NewInt(10000), Sources: []Distribution{
		{Identifier: "balance_a", Distribution: "60"},
		{Identifier: "balance_a", Distribution: "40"},
	}}
	_, err := txn.SplitTransactionPrecise(context.Background())
	require.ErrorContains(t, err, "unique")
	for _, dist := range txn.Sources {
		assert.Empty(t, dist.TransactionID)
	}
}

// Fan-out must behave identically to fan-in.
func TestSplitTransactionPrecise_DestinationsAttributedToTheirOwnEntry(t *testing.T) {
	const runs = 100
	for i := 0; i < runs; i++ {
		txn := &Transaction{
			TransactionID: "txn_parent",
			Reference:     "ref",
			Precision:     100,
			PreciseAmount: big.NewInt(10000),
			Amount:        100,
			Currency:      "USD",
			Source:        "bln_src",
			Destinations: []Distribution{
				{Identifier: "bln_X", Distribution: "70%"},
				{Identifier: "bln_Y", Distribution: "30%"},
			},
		}
		legs, err := txn.SplitTransactionPrecise(context.Background())
		require.NoError(t, err)
		require.Len(t, legs, 2)

		byDest := make(map[string]*Transaction, len(legs))
		for _, leg := range legs {
			byDest[leg.Destination] = leg
		}
		for _, d := range txn.Destinations {
			leg := byDest[d.Identifier]
			require.NotNil(t, leg)
			require.Equal(t, leg.TransactionID, d.TransactionID)
		}
	}
}
