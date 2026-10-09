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
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/devaccuracy/ledgerforge/internal/request"
	"github.com/devaccuracy/ledgerforge/model"
)

// TestRefundAppliesDescriptionAndMetaData covers the endpoint contract: the
// reversal can be described and classified as its own movement rather than
// inheriting the original's.
func TestRefundAppliesDescriptionAndMetaData(t *testing.T) {
	router, b, err := setupRouter(t)
	require.NoError(t, err)

	source, destination := newDryRunFixture(t, b)

	applied, err := b.QueueTransaction(t.Context(), &model.Transaction{
		Reference:   "refopt_" + model.GenerateUUIDWithSuffix("ref"),
		Source:      source,
		Destination: destination,
		Amount:      100,
		Precision:   100,
		Currency:    "USD",
		Description: "customer deposit",
		SkipQueue:   true,
		MetaData:    map[string]interface{}{"type": "deposit", "channel": "card"},
	})
	require.NoError(t, err)

	body, err := request.ToJsonReq(&map[string]interface{}{
		"skip_queue":  true,
		"description": "refund for ticket #4821",
		"meta_data":   map[string]interface{}{"type": "refund", "reason": "goodwill"},
	})
	require.NoError(t, err)

	var refund model.Transaction
	resp, err := SetUpTestRequest(TestRequest{
		Payload:  body,
		Response: &refund,
		Method:   http.MethodPost,
		Route:    "/refund-transaction/" + applied.TransactionID,
		Router:   router,
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.Code)

	assert.Equal(t, "refund for ticket #4821", refund.Description)
	assert.Equal(t, "refund", refund.MetaData["type"], "the caller's classification must win")
	assert.Equal(t, "goodwill", refund.MetaData["reason"])
	assert.Equal(t, "card", refund.MetaData["channel"], "keys not overridden are still inherited")

	// The original must be untouched.
	original, err := b.GetTransaction(t.Context(), applied.TransactionID)
	require.NoError(t, err)
	assert.Equal(t, "customer deposit", original.Description)
	assert.Equal(t, "deposit", original.MetaData["type"], "refunding must not rewrite the original")
}

// TestRefundWithoutOverridesInherits pins the unchanged default: an absent
// description and metadata reproduce today's behaviour exactly.
func TestRefundWithoutOverridesInherits(t *testing.T) {
	router, b, err := setupRouter(t)
	require.NoError(t, err)

	source, destination := newDryRunFixture(t, b)

	applied, err := b.QueueTransaction(t.Context(), &model.Transaction{
		Reference:   "refplain_" + model.GenerateUUIDWithSuffix("ref"),
		Source:      source,
		Destination: destination,
		Amount:      100,
		Precision:   100,
		Currency:    "USD",
		Description: "customer deposit",
		SkipQueue:   true,
		MetaData:    map[string]interface{}{"type": "deposit"},
	})
	require.NoError(t, err)

	body, err := request.ToJsonReq(&map[string]interface{}{"skip_queue": true})
	require.NoError(t, err)

	var refund model.Transaction
	resp, err := SetUpTestRequest(TestRequest{
		Payload:  body,
		Response: &refund,
		Method:   http.MethodPost,
		Route:    "/refund-transaction/" + applied.TransactionID,
		Router:   router,
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.Code)

	assert.Equal(t, "customer deposit", refund.Description)
	assert.Equal(t, "deposit", refund.MetaData["type"])
}
