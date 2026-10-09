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

	"github.com/devaccuracy/ledgerforge/model"
	"github.com/stretchr/testify/assert"
)

func TestResolveAPIKeyOwner(t *testing.T) {
	principal := &model.APIKey{OwnerID: "owner_a"}

	tests := []struct {
		name           string
		isMaster       bool
		principal      *model.APIKey
		requestedOwner string
		ownerRequired  bool
		expectedOwner  string
		expectedErr    error
	}{
		{
			name:           "Master key can manage requested owner",
			isMaster:       true,
			requestedOwner: "owner_b",
			ownerRequired:  true,
			expectedOwner:  "owner_b",
		},
		{
			name:          "Master key requires owner when route depends on it",
			isMaster:      true,
			ownerRequired: true,
			expectedErr:   errAPIKeyOwnerRequired,
		},
		{
			name:          "API key uses its own owner when query owner omitted",
			principal:     principal,
			expectedOwner: "owner_a",
		},
		{
			name:           "API key can manage matching owner",
			principal:      principal,
			requestedOwner: "owner_a",
			expectedOwner:  "owner_a",
		},
		{
			name:           "API key cannot manage another owner",
			principal:      principal,
			requestedOwner: "owner_b",
			expectedErr:    errAPIKeyCrossOwnerAccess,
		},
		{
			name:        "API key principal is required",
			expectedErr: errMissingAPIKeyPrincipal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ownerID, err := resolveAPIKeyOwner(tt.isMaster, tt.principal, tt.requestedOwner, tt.ownerRequired)
			assert.ErrorIs(t, err, tt.expectedErr)
			assert.Equal(t, tt.expectedOwner, ownerID)
		})
	}
}

func TestValidateGrantedScopes(t *testing.T) {
	principal := &model.APIKey{
		OwnerID: "owner_a",
		Scopes:  []string{"api-keys:write", "api-keys:read", "ledgers:*"},
	}

	tests := []struct {
		name            string
		isMaster        bool
		principal       *model.APIKey
		requestedScopes []string
		expectedErr     error
	}{
		{
			name:            "Master key can grant any scope",
			isMaster:        true,
			requestedScopes: []string{"*:*"},
		},
		{
			name:            "API key can grant exact owned scopes",
			principal:       principal,
			requestedScopes: []string{"api-keys:read"},
		},
		{
			name:            "API key can grant scopes covered by wildcard action",
			principal:       principal,
			requestedScopes: []string{"ledgers:delete"},
		},
		{
			name:            "API key cannot grant broader scopes",
			principal:       principal,
			requestedScopes: []string{"*:*"},
			expectedErr:     errAPIKeyScopeEscalation,
		},
		{
			name:            "API key cannot grant other resource scopes",
			principal:       principal,
			requestedScopes: []string{"accounts:write"},
			expectedErr:     errAPIKeyScopeEscalation,
		},
		{
			name:            "API key cannot grant invalid scopes",
			principal:       principal,
			requestedScopes: []string{"not-a-scope"},
			expectedErr:     errAPIKeyScopeEscalation,
		},
		{
			name:            "API key principal is required",
			requestedScopes: []string{"ledgers:read"},
			expectedErr:     errMissingAPIKeyPrincipal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateGrantedScopes(tt.isMaster, tt.principal, tt.requestedScopes)
			assert.ErrorIs(t, err, tt.expectedErr)
		})
	}
}
