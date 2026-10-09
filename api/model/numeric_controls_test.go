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
	"fmt"
	"testing"

	ledgerModel "github.com/devaccuracy/ledgerforge/model"
	"github.com/stretchr/testify/require"
)

func TestNumericControlTokensPreserveBoundary(t *testing.T) {
	for _, tc := range []struct {
		token   string
		valid   bool
		integer bool
	}{
		{"9007199254740991", true, true}, {"9007199254740992", true, true}, {"9007199254740993", false, true}, {"9007199254740994", true, true},
		{"9.007199254740993e15", false, true}, {"100.000000000000000001", false, true}, {"9007199254740992.1", false, true}, {"1e18", true, true}, {"1e24", false, true}, {"1000000000000000100", false, true}, {"1208925819614629174706176", false, true}, {"100.0", true, true}, {"1e-400", false, true},
		{"0.29", true, false}, {"1000000000000000100", false, false}, {"9007199254740992", true, false}, {"9007199254740993", false, false}, {"9007199254740992.5", false, false}, {"0.290000000000000000001", false, false}, {"1e-400", false, false},
	} {
		field := "overdraft_limit"
		if tc.integer {
			field = "precision"
		}
		t.Run(field+"/"+tc.token, func(t *testing.T) {
			var req RecordTransaction
			require.NoError(t, json.Unmarshal([]byte(fmt.Sprintf(`{"amount":1,"currency":"USD","reference":"ref","description":"test","source":"source","destination":"destination",%q:%s}`, field, tc.token)), &req))
			err := req.ValidateRecordTransaction()
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestBalanceAndMonitorPrecisionTokens(t *testing.T) {
	for _, token := range []string{"9007199254740993", "100.000000000000000001", "1e-400"} {
		t.Run(token, func(t *testing.T) {
			var balance CreateBalance
			require.NoError(t, json.Unmarshal([]byte(`{"ledger_id":"ledger","currency":"USD","precision":`+token+`}`), &balance))
			require.Error(t, balance.ValidateCreateBalance())
			var monitor CreateBalanceMonitor
			require.NoError(t, json.Unmarshal([]byte(`{"balance_id":"balance","condition":{"field":"balance","operator":">","value":1,"precision":`+token+`}}`), &monitor))
			require.Error(t, monitor.ValidateCreateBalanceMonitor())
		})
	}
	var req RecordTransaction
	require.NoError(t, json.Unmarshal([]byte(`{"precision":9007199254740993,"overdraft_limit":1e-400}`), &req))
	require.NoError(t, json.Unmarshal([]byte(`{"precision":100,"overdraft_limit":0.29}`), &req))
	require.NoError(t, validateExactControl(req.controls.precision, req.Precision, true))
	require.NoError(t, validateExactControl(req.controls.overdraftLimit, req.OverdraftLimit, false))
	require.NoError(t, json.Unmarshal([]byte(`{}`), &req))
	require.Equal(t, float64(100), req.Precision)
	require.Equal(t, 0.29, req.OverdraftLimit)
	require.NoError(t, validateExactControl(req.controls.precision, req.Precision, true))
}

func TestNumericControlNullDoesNotEraseLossyToken(t *testing.T) {
	for _, body := range []string{
		`{"precision":9007199254740993,"precision":null}`,
		`{"overdraft_limit":9007199254740993,"overdraft_limit":null}`,
		`{"overdraft_limit":1e-400,"overdraft_limit":null}`,
	} {
		t.Run(body, func(t *testing.T) {
			req := RecordTransaction{Amount: 1, Currency: "USD", Reference: "ref", Description: "test", Source: "source", Destination: "destination"}
			require.NoError(t, json.Unmarshal([]byte(body), &req))
			require.Error(t, req.ValidateRecordTransaction())
		})
	}
}

func TestJSONNullPreservesExactAmountToken(t *testing.T) {
	var req RecordTransaction
	const exact = "1.004999999999999999999"
	require.NoError(t, json.Unmarshal([]byte(`{"amount":`+exact+`,"amount":null,"precision":100}`), &req))
	require.Equal(t, exact, req.ExactAmount())
	minor, err := ledgerModel.ApplyPrecisionChecked(req.ToTransaction())
	require.NoError(t, err)
	require.Equal(t, "100", minor.String())
}
