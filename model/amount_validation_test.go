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
	"fmt"
	"math"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseAmountRounding(t *testing.T) {
	for _, tc := range []struct {
		amount    string
		precision float64
		want      string
	}{
		{"1.005", 100, "101"}, {"1.004999999999999999999", 100, "100"},
		{"0.005", 100, "1"}, {"0.0049", 100, ""}, {"0.29", 100, "29"},
		{"1.5", 0, "2"}, {"9223372036854775808.01", 100, "922337203685477580801"},
		{"1e-18", 1e18, "1"}, {"-1", 100, ""}, {"NaN", 100, ""},
		{"Inf", 100, ""}, {"1", 1.5, ""}, {"1", -100, ""},
		{"1", math.NaN(), ""}, {"1", math.Inf(1), ""},
		{"1e2147483647", 100, ""}, {".+0270000000000000000", 100, ""},
	} {
		t.Run(tc.amount+"/"+fmt.Sprint(tc.precision), func(t *testing.T) {
			got, err := ParseAmount(tc.amount, tc.precision)
			if tc.want == "" {
				require.Error(t, err)
				require.Nil(t, got)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.want, got.String())
			}
		})
	}
}

func TestMonetaryValidationBeforeConversion(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*Transaction)
	}{
		{"negative amount despite precise", func(tx *Transaction) { tx.Amount = -1; tx.PreciseAmount = big.NewInt(100) }},
		{"negative decimal despite precise", func(tx *Transaction) { tx.AmountString = "-1"; tx.PreciseAmount = big.NewInt(100) }},
		{"malformed decimal despite precise", func(tx *Transaction) { tx.AmountString = "NaN"; tx.PreciseAmount = big.NewInt(100) }},
		{"negative precise", func(tx *Transaction) { tx.PreciseAmount = big.NewInt(-1) }},
		{"zero precise", func(tx *Transaction) { tx.PreciseAmount = big.NewInt(0) }},
		{"NaN amount", func(tx *Transaction) { tx.Amount = math.NaN() }},
		{"infinite amount", func(tx *Transaction) { tx.Amount = math.Inf(1) }},
		{"negative precision", func(tx *Transaction) { tx.Precision = -100 }},
		{"fractional precision", func(tx *Transaction) { tx.Precision = 1.5 }},
		{"NaN precision", func(tx *Transaction) { tx.Precision = math.NaN() }},
		{"infinite precision", func(tx *Transaction) { tx.Precision = math.Inf(1) }},
		{"negative rate", func(tx *Transaction) { tx.Rate = -1 }},
		{"NaN rate", func(tx *Transaction) { tx.Rate = math.NaN() }},
		{"infinite rate", func(tx *Transaction) { tx.Rate = math.Inf(1) }},
		{"negative overdraft", func(tx *Transaction) { tx.OverdraftLimit = -1 }},
		{"NaN overdraft", func(tx *Transaction) { tx.OverdraftLimit = math.NaN() }},
		{"infinite overdraft", func(tx *Transaction) { tx.OverdraftLimit = math.Inf(1) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := &Transaction{Amount: 1, Precision: 100, AllowOverdraft: true}
			tc.edit(tx)
			before := tx.PreciseAmount
			source, destination := &Balance{}, &Balance{}
			require.Error(t, tx.ValidateAmount())
			require.Error(t, UpdateBalances(tx, source, destination))
			require.Same(t, before, tx.PreciseAmount)
			require.Nil(t, source.Balance)
			require.Nil(t, destination.Balance)
		})
	}
	for _, tx := range []*Transaction{{Amount: 1}, {PreciseAmount: big.NewInt(1), Precision: 100}, {Amount: 1, Precision: 100, Rate: 2, OverdraftLimit: 0}} {
		_, err := ApplyPrecisionChecked(tx)
		require.NoError(t, err)
	}
}

func TestPreciseAmountDecimalBeyondSixteenPlaces(t *testing.T) {
	for _, precision := range []float64{1e18, 1e30, 1e100, 1e300} {
		tx := &Transaction{PreciseAmount: big.NewInt(1), Precision: precision}
		_, err := ApplyPrecisionChecked(tx)
		require.NoError(t, err)
		minor, err := ParseAmount(tx.AmountString, precision)
		require.NoError(t, err)
		require.Equal(t, "1", minor.String())
	}
}

func TestLargeOverdraftLimitDoesNotOverflowInt64(t *testing.T) {
	amount, _ := new(big.Int).SetString("100000000000000000000", 10)
	tx := &Transaction{PreciseAmount: amount, Precision: 100, OverdraftLimit: 1e18}
	require.NoError(t, canProcessTransaction(tx, &Balance{}))
	tx.PreciseAmount = new(big.Int).Add(amount, big.NewInt(1))
	require.Error(t, canProcessTransaction(tx, &Balance{}))
}
