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

package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/devaccuracy/ledgerforge/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMetadataInjectionPreservesNumericTokens(t *testing.T) {
	for _, body := range []string{
		`{"precise_amount":9007199254740993,"amount":9007199254740993.0001,"meta_data":{"numeric":123456789012345678901234567890}}`,
		`{"transactions":[{"precise_amount":9007199254740993},{"precise_amount":123456789012345678901234567890,"meta_data":{"amount":9.007199254740993e20}}]}`,
	} {
		t.Run(body, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/transactions", strings.NewReader(body))
			require.NoError(t, injectAPIKeyToMetadata(c, "key-id"))
			actual, err := io.ReadAll(c.Request.Body)
			require.NoError(t, err)
			var before, after map[string]json.RawMessage
			require.NoError(t, json.Unmarshal([]byte(body), &before))
			require.NoError(t, json.Unmarshal(actual, &after))
			for key, raw := range before {
				if key != "meta_data" {
					require.JSONEq(t, string(raw), string(after[key]))
					require.Equal(t, bytes.TrimSpace(raw), bytes.TrimSpace(after[key]))
				}
			}
			require.Contains(t, string(actual), `123456789012345678901234567890`)
			if strings.Contains(body, `"numeric"`) {
				require.Contains(t, string(actual), `"numeric":123456789012345678901234567890`)
			}
			require.Contains(t, string(actual), `"LEDGERFORGE_GENERATED_BY":"key-id"`)
		})
	}
}

func TestMetadataInjectionBoundsBodyWithoutSizeMiddleware(t *testing.T) {
	config.ConfigStore.Store(&config.Configuration{Server: config.ServerConfig{MaxRequestBodySizeMB: 1}})
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/transactions", strings.NewReader(strings.Repeat(" ", 1024*1024+1)))
	err := injectAPIKeyToMetadata(c, "id")
	var tooLarge *http.MaxBytesError
	require.True(t, errors.As(err, &tooLarge))
}

func TestSecurityRouteResources(t *testing.T) {
	for path, expected := range map[string]Resource{
		"/balances-snapshots":          ResourceBalances,
		"/backup-s3":                   ResourceBackup,
		"/balance_123/metadata":        ResourceMetadata,
		"/transactions/txn_1/metadata": ResourceMetadata,
	} {
		require.Equal(t, expected, getResourceFromPath(path))
		require.True(t, HasPermission([]string{string(expected) + ":*"}, getResourceFromPath(path), "POST"))
	}
}
