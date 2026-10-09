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
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/devaccuracy/ledgerforge/model"
	"github.com/stretchr/testify/require"
)

func TestNumericControlBoundaryHTTP(t *testing.T) {
	router, b, err := setupRouter(t)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, b.Close()) })
	source, destination := newDryRunFixture(t, b)
	for _, tc := range []struct {
		name, control string
		accepted      bool
	}{
		{"precision below boundary", `"precision":9007199254740991`, true},
		{"precision at boundary", `"precision":9007199254740992`, true},
		{"lossy precision above boundary", `"precision":9007199254740993`, false},
		{"exact precision above boundary", `"precision":9007199254740994`, true},
		{"precision binary rounding behind matching decimal text", `"precision":1000000000000000100`, false},
		{"fractional precision rounded to integer", `"precision":100.000000000000000001`, false},
		{"ordinary fractional overdraft", `"overdraft_limit":0.29`, true},
		{"overdraft at boundary", `"overdraft_limit":9007199254740992`, true},
		{"lossy overdraft above boundary", `"overdraft_limit":9007199254740993`, false},
		{"overdraft binary rounding behind matching decimal text", `"overdraft_limit":1000000000000000100`, false},
		{"overdraft fraction rounded away", `"overdraft_limit":9007199254740992.5`, false},
		{"overdraft underflow to unlimited", `"overdraft_limit":1e-400`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ref := model.GenerateUUIDWithSuffix("numeric_control")
			before, err := b.GetBalanceByID(t.Context(), source, nil, false)
			require.NoError(t, err)
			body := []byte(fmt.Sprintf(`{"source":%q,"destination":%q,"reference":%q,"description":"numeric boundary","currency":"USD","precise_amount":1,"allow_overdraft":true,"skip_queue":true,%s}`, source, destination, ref, tc.control))
			response := doJSONRequest(router, http.MethodPost, "/transactions", body)
			if tc.accepted {
				require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
				var posted model.Transaction
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &posted))
				require.Equal(t, "1", posted.PreciseAmount.String())
				stored, err := b.GetTransaction(t.Context(), posted.TransactionID)
				require.NoError(t, err)
				require.Equal(t, posted.Precision, stored.Precision)
				require.Equal(t, posted.OverdraftLimit, stored.OverdraftLimit)
			} else {
				require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
				after, err := b.GetBalanceByID(t.Context(), source, nil, false)
				require.NoError(t, err)
				require.Equal(t, before, after)
				_, err = b.GetTransactionByRef(t.Context(), ref)
				require.Error(t, err, "lossy controls must never be persisted")
			}
		})
	}
}
