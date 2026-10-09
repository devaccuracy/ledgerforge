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
	"errors"
	"net/http"
	"testing"

	"github.com/devaccuracy/ledgerforge/internal/apierror"
	"github.com/stretchr/testify/require"
)

func TestPreciseAmountValidationMessageRemainsFieldSpecific(t *testing.T) {
	for _, text := range []string{"precise_amount must be positive", "transaction precise amount must be positive", "precise_amount must be positive: pq: sensitive backend detail"} {
		c, w := newTestContext()
		respondError(c, errors.New(text))
		require.Equal(t, http.StatusBadRequest, w.Code)
		raw, detail := decodeDetail(t, w)
		require.Equal(t, "precise_amount must be positive", raw["error"])
		require.Equal(t, apierror.ErrTxnInvalidAmount, detail.Code)
		require.NotContains(t, w.Body.String(), "sensitive backend detail")
	}
}
