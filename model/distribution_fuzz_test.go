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
)

func FuzzDistributionParsingConservesTotal(f *testing.F) {
	for _, component := range []string{"50%", "0.29", "1.005", "left", "-1", "NaN", "33.333333333333333333%", "9223372036854775808"} {
		f.Add(component, uint64(10001), uint16(100), false)
	}
	f.Add("9223372036854775808", uint64(9223372036854775908), uint16(100), true)
	f.Fuzz(func(t *testing.T, component string, total uint64, scale uint16, precise bool) {
		if scale == 0 {
			scale = 1
		}
		amount := new(big.Int).SetUint64(total)
		first := Distribution{Identifier: "a", Distribution: component}
		if precise {
			first.Distribution = ""
			first.PreciseDistribution = component
		}
		dists := []Distribution{first, {Identifier: "b", Distribution: "left"}}
		got, err := CalculateDistributionsPrecise(context.Background(), amount, dists, int64(scale))
		if err != nil {
			return
		}
		sum := new(big.Int)
		for _, value := range got {
			if value.Sign() < 0 {
				t.Fatal("negative allocation")
			}
			sum.Add(sum, value)
		}
		if sum.Cmp(amount) != 0 {
			t.Fatalf("%q: sum %s, total %s", component, sum, amount)
		}
		reversed, err := CalculateDistributionsPrecise(context.Background(), amount, []Distribution{dists[1], dists[0]}, int64(scale))
		if err != nil {
			t.Fatal(err)
		}
		for id, value := range got {
			if reversed[id] == nil || value.Cmp(reversed[id]) != 0 {
				t.Fatalf("allocation depends on input order: %q", component)
			}
		}
		if precise && first.PreciseDistribution != "left" {
			want, ok := new(big.Int).SetString(component, 10)
			if !ok || want.Sign() < 0 {
				t.Fatal("accepted invalid precise distribution")
			}
			if got["a"] == nil || got["a"].Cmp(want) != 0 {
				t.Fatalf("integer distribution changed: %q", component)
			}
		}
	})
}
