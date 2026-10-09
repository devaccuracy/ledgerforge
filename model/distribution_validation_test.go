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

	"github.com/stretchr/testify/require"
)

func TestDistributionValidation(t *testing.T) {
	for _, tc := range []struct {
		name      string
		total     int64
		precision int64
		dists     []Distribution
	}{
		{"negative fixed", 100, 100, []Distribution{{Identifier: "a", Distribution: "-0.01"}, {Identifier: "b", Distribution: "left"}}},
		{"negative precise", 100, 100, []Distribution{{Identifier: "a", PreciseDistribution: "-1"}, {Identifier: "b", Distribution: "left"}}},
		{"negative percent", 100, 100, []Distribution{{Identifier: "a", Distribution: "-1%"}, {Identifier: "b", Distribution: "left"}}},
		{"duplicates", 100, 100, []Distribution{{Identifier: "a", Distribution: "50%"}, {Identifier: "a", Distribution: "50%"}}},
		{"multiple left", 100, 100, []Distribution{{Identifier: "a", Distribution: "left"}, {Identifier: "b", Distribution: "left"}}},
		{"ambiguous amounts", 100, 100, []Distribution{{Identifier: "a", Distribution: "1", PreciseDistribution: "1"}}},
		{"fixed plus percent exceeds total", 100, 100, []Distribution{{Identifier: "a", Distribution: "0.6"}, {Identifier: "b", Distribution: "50%"}}},
		{"fractional components exceed total", 1, 100, []Distribution{{Identifier: "a", Distribution: "0.009"}, {Identifier: "b", Distribution: "20%"}}},
		{"invalid tiny total", 1, 100, []Distribution{{Identifier: "a", Distribution: "abc%"}}},
		{"invalid zero total", 0, 100, []Distribution{{Identifier: "a", Distribution: "-1%"}}},
		{"empty identifier", 100, 100, []Distribution{{Distribution: "left"}}},
		{"empty list", 100, 100, nil},
		{"negative total", -1, 100, []Distribution{{Identifier: "a", Distribution: "left"}}},
		{"zero scale", 100, 0, []Distribution{{Identifier: "a", Distribution: "left"}}},
		{"oversized exponent", 100, 100, []Distribution{{Identifier: "a", Distribution: "1e2147483647"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CalculateDistributionsPrecise(context.Background(), big.NewInt(tc.total), tc.dists, tc.precision)
			require.Error(t, err)
		})
	}
	_, err := CalculateDistributionsPrecise(context.Background(), nil, []Distribution{{Identifier: "a", Distribution: "left"}}, 100)
	require.Error(t, err)
}

func TestDistributionExactDecimalAndDeterministicTies(t *testing.T) {
	total, _ := new(big.Int).SetString("922337203685477580901", 10)
	dists := []Distribution{{Identifier: "large", Distribution: "9223372036854775808.01"}, {Identifier: "rest", Distribution: "left"}}
	result, err := CalculateDistributionsPrecise(context.Background(), total, dists, 100)
	require.NoError(t, err)
	require.Equal(t, "922337203685477580801", result["large"].String())
	require.Equal(t, "100", result["rest"].String())
	for _, total := range []int64{1, 101} {
		for i := 0; i < 100; i++ {
			dists := []Distribution{{Identifier: "b", Distribution: "50%"}, {Identifier: "a", Distribution: "50%"}}
			if i%2 == 0 {
				dists[0], dists[1] = dists[1], dists[0]
			}
			result, err := CalculateDistributionsPrecise(context.Background(), big.NewInt(total), dists, 100)
			require.NoError(t, err)
			require.Equal(t, big.NewInt(total/2+1), result["a"])
			require.Equal(t, big.NewInt(total/2), result["b"])
		}
	}
	// Percentages differing beyond float64 accuracy must not be rounded to 100%.
	_, err = CalculateDistributionsPrecise(context.Background(), big.NewInt(100), []Distribution{{Identifier: "a", Distribution: "100.000000000000000001%"}}, 100)
	require.Error(t, err)
}
