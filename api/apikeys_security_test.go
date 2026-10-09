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
	"testing"
	"time"

	"github.com/devaccuracy/ledgerforge/model"
	"github.com/stretchr/testify/require"
)

func TestDelegatedAPIKeyExpiry(t *testing.T) {
	expiry := time.Now().Add(time.Hour)
	principal := &model.APIKey{ExpiresAt: expiry}
	require.NoError(t, validateGrantedExpiry(false, principal, expiry.Add(-time.Second)))
	require.NoError(t, validateGrantedExpiry(false, principal, expiry))
	require.ErrorIs(t, validateGrantedExpiry(false, principal, expiry.Add(time.Nanosecond)), errAPIKeyExpiryEscalation)
	require.ErrorIs(t, validateGrantedExpiry(false, principal, time.Time{}), errAPIKeyExpiryEscalation)
	require.NoError(t, validateGrantedExpiry(true, nil, expiry.Add(time.Hour)))
}

func TestDelegatedAPIKeyCannotMintWildcard(t *testing.T) {
	principal := &model.APIKey{Scopes: []string{"*:*"}}
	for _, scopes := range [][]string{{"*:*"}, {"*:read"}, {"ledgers:*"}} {
		require.ErrorIs(t, validateGrantedScopes(false, principal, scopes), errAPIKeyScopeEscalation)
	}
	require.NoError(t, validateGrantedScopes(false, principal, []string{"ledgers:read"}))
}
