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
	"math/big"
	"testing"
)

func FuzzParseAmountExactRounding(f *testing.F) {
	for _, amount := range []string{"1.005", "0.29", "0.004", "9223372036854775808.01", "1e-18", "-1", "NaN", "1e2147483647", ".+0270000000000000000"} {
		f.Add(amount, uint16(100))
	}
	f.Fuzz(func(t *testing.T, text string, scale uint16) {
		precision := float64(scale)
		got, err := ParseAmount(text, precision)
		if err != nil {
			return
		}
		// An independent rational oracle must agree exactly, including half units.
		value, ok := new(big.Rat).SetString(text)
		if !ok || value.Sign() <= 0 {
			t.Fatalf("accepted invalid amount %q", text)
		}
		if scale == 0 {
			scale = 1
		}
		value.Mul(value, new(big.Rat).SetInt64(int64(scale)))
		quotient, remainder := new(big.Int), new(big.Int)
		quotient.QuoRem(value.Num(), value.Denom(), remainder)
		if remainder.Lsh(remainder, 1).Cmp(value.Denom()) >= 0 {
			quotient.Add(quotient, big.NewInt(1))
		}
		if got.Sign() <= 0 || got.Cmp(quotient) != 0 {
			t.Fatalf("%q x %d: got %s, want %s", text, scale, got, quotient)
		}
	})
}
