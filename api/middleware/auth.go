/*
Copyright 2024 Blnk Finance Authors.

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
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/devaccuracy/ledgerforge"
	"github.com/devaccuracy/ledgerforge/config"
	"github.com/devaccuracy/ledgerforge/database"
	"github.com/devaccuracy/ledgerforge/internal/apierror"
	"github.com/devaccuracy/ledgerforge/internal/policy"
	"github.com/gin-gonic/gin"
)

const (
	KeyHeader = "X-LedgerForge-Key"
)

// abortWithCode writes the standard dual error payload (legacy flat "error"
// string + structured "error_detail") and aborts the request. The status is
// resolved from the error-code catalog.
func abortWithCode(c *gin.Context, code apierror.ErrorCode, message string) {
	resp := apierror.NewErrorResponse(code, message, nil)
	c.AbortWithStatusJSON(apierror.StatusForCode(resp.Error.Code), gin.H{
		"error":        message,
		"error_detail": resp.Error,
	})
}

// pathToResource maps URL paths to their corresponding resource types.
// This is used by the authentication middleware to determine the required permissions.
var pathToResource = map[string]Resource{
	"ledgers":            ResourceLedgers,
	"balances":           ResourceBalances,
	"balances-snapshots": ResourceBalances,
	"accounts":           ResourceAccounts,
	"identities":         ResourceIdentities,
	"transactions":       ResourceTransactions,
	"balance-monitors":   ResourceBalanceMonitors,
	"hooks":              ResourceHooks,
	"api-keys":           ResourceAPIKeys,
	"policies":           Resource("policies"),
	"approvals":          Resource("approvals"),
	"search":             ResourceSearch,
	"reconciliation":     ResourceReconciliation,
	"metadata":           ResourceMetadata,
	"backup":             ResourceBackup,
	"backup-s3":          ResourceBackup,
}

// AuthMiddleware handles authentication and authorization for API routes.
// It supports both master key and API key authentication using the X-LedgerForge-Key header.
type AuthMiddleware struct {
	service *ledgerforge.LedgerForge
}

// NewAuthMiddleware creates a new instance of AuthMiddleware.
//
// Parameters:
// - ledgerforge: The LedgerForge service used to validate API keys.
//
// Returns:
// - *AuthMiddleware: A new instance of the authentication middleware.
func NewAuthMiddleware(ledgerforge *ledgerforge.LedgerForge) *AuthMiddleware {
	return &AuthMiddleware{service: ledgerforge}
}

// getResourceFromPath determines the resource type from the URL path.
//
// Parameters:
// - path: The URL path to analyze.
//
// Returns:
// - Resource: The determined resource type, or empty string if not found.
func getResourceFromPath(path string) Resource {
	// Remove leading slash and get first path segment
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) == 0 {
		return ""
	}

	// Special case for mocked-account
	if parts[0] == "mocked-account" {
		return ResourceAccounts
	}

	// Special case for multi-search
	if parts[0] == "multi-search" {
		return ResourceSearch
	}

	if parts[0] == "refund-transaction" {
		return ResourceTransactions
	}

	// Treat paths like /entity-id/metadata as the metadata resource.
	if parts[len(parts)-1] == "metadata" {
		return ResourceMetadata
	}

	// Check if the path segment maps to a known resource
	if resource, ok := pathToResource[parts[0]]; ok {
		return resource
	}

	return ""
}

// injectAPIKeyToMetadata modifies the request body to include the API key ID in the meta_data.
// This function reads the request body, adds or updates the meta_data field, and sets the modified
// body back to the request.
//
// Parameters:
// - c: The Gin context containing the request.
// - apiKeyID: The API key ID to inject into the metadata.
//
// Returns:
// - error: An error if the body processing fails.
func injectAPIKeyToMetadata(c *gin.Context, apiKeyID string) error {
	// Only proceed if this is a POST request
	if c.Request.Method != "POST" {
		return nil
	}

	// Check if the request body is nil
	if c.Request.Body == nil {
		return nil
	}

	// Multipart and form submissions contain no JSON metadata.
	if strings.HasPrefix(c.GetHeader("Content-Type"), "multipart/form-data") || strings.HasPrefix(c.GetHeader("Content-Type"), "application/x-www-form-urlencoded") {
		return nil
	}
	maxBytes := int64(config.DEFAULT_MAX_REQUEST_BODY_SIZE_MB) * 1024 * 1024
	if conf, err := config.Fetch(); err == nil && conf.Server.MaxRequestBodySizeMB > 0 {
		maxBytes = conf.Server.MaxRequestBodySizeMB * 1024 * 1024
	}
	bodyBytes, err := io.ReadAll(io.LimitReader(c.Request.Body, maxBytes+1))
	if err != nil {
		return err
	}
	_ = c.Request.Body.Close()
	if int64(len(bodyBytes)) > maxBytes {
		return &http.MaxBytesError{Limit: maxBytes}
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	if len(bytes.TrimSpace(bodyBytes)) == 0 {
		return nil
	}
	// RawMessage preserves all numeric tokens, including nested bulk amounts.
	var bodyMap map[string]json.RawMessage
	if err := json.Unmarshal(bodyBytes, &bodyMap); err != nil {
		return err
	}
	if bodyMap == nil {
		return errors.New("request body must be a JSON object")
	}
	var metaData map[string]json.RawMessage
	_ = json.Unmarshal(bodyMap["meta_data"], &metaData)
	if metaData == nil {
		metaData = make(map[string]json.RawMessage)
	}
	metaData["LEDGERFORGE_GENERATED_BY"], _ = json.Marshal(apiKeyID)
	bodyMap["meta_data"], _ = json.Marshal(metaData)

	// Convert back to JSON
	modifiedBody, err := json.Marshal(bodyMap)
	if err != nil {
		// If marshaling fails, restore the original body and continue
		c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		return err
	}

	// Set the modified body back to the request
	c.Request.Body = io.NopCloser(bytes.NewBuffer(modifiedBody))
	c.Request.ContentLength = int64(len(modifiedBody))

	return nil
}

// Authenticate returns a middleware function that handles authentication and authorization for all routes.
// It checks for the X-LedgerForge-Key header and validates it against either the master key or API keys.
// For API keys, it verifies the key's validity and checks permissions based on the resource and HTTP method.
// For POST requests with API keys, it injects the API key ID into the metadata of the request body.
//
// Returns:
// - gin.HandlerFunc: A middleware function that performs the authentication.
//
// Responses:
// - 200 OK: When authentication succeeds.
// - 401 Unauthorized: When the API key is missing or invalid.
// - 403 Forbidden: When the API key lacks sufficient permissions.
func (m *AuthMiddleware) Authenticate() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Skip auth for root path
		if c.Request != nil && c.Request.URL != nil && c.Request.URL.Path == "/" {
			c.Next()
			return
		}

		// Skip auth for health endpoint (server health check only)
		if c.Request != nil && c.Request.URL != nil && c.Request.URL.Path == "/health" {
			c.Next()
			return
		}

		// Skip X-LedgerForge-Key auth for metrics endpoint, it uses its own bearer token auth
		if c.Request != nil && c.Request.URL != nil && c.Request.URL.Path == "/metrics" {
			c.Next()
			return
		}

		// Check if secure mode is enabled
		conf, err := config.Fetch()
		if err == nil && conf != nil && !conf.Server.Secure {
			// Skip authentication when secure mode is disabled
			c.Next()
			return
		}

		key := extractKey(c)
		if key == "" {
			abortWithCode(c, apierror.ErrAuthMissingAPIKey, "Authentication required. Use X-LedgerForge-Key header")
			return
		}

		// First check if it's the master key
		if err == nil && conf != nil && conf.Server.SecretKey != "" &&
			subtle.ConstantTimeCompare([]byte(conf.Server.SecretKey), []byte(key)) == 1 {
			// Master key has all permissions
			c.Set("isMasterKey", true)
			c.Request = c.Request.WithContext(policy.WithPrincipal(c.Request.Context(), policy.Principal{Master: true}))
			c.Next()
			return
		}

		// If not master key, try API key authentication
		apiKey, err := m.service.GetAPIKeyByKey(c.Request.Context(), key)
		if errors.Is(err, database.ErrLegacyAPIKeyVerificationBusy) {
			abortWithCode(c, apierror.ErrGenRateLimited, "legacy API key verification is busy; retry later")
			return
		}
		if err != nil {
			abortWithCode(c, apierror.ErrAuthInvalidAPIKey, "Invalid API key")
			return
		}

		if !apiKey.IsValid() {
			abortWithCode(c, apierror.ErrAuthExpiredAPIKey, "API key is expired or revoked")
			return
		}

		// Determine required resource from path
		if c.Request == nil || c.Request.URL == nil {
			abortWithCode(c, apierror.ErrGenInternal, "Invalid request")
			return
		}

		resource := getResourceFromPath(c.Request.URL.Path)
		if resource == "" {
			abortWithCode(c, apierror.ErrAuthUnknownResource, "Unknown resource type")
			return
		}

		// Check if API key has permission for this resource and method
		if resource != Resource("policies") && resource != Resource("approvals") && !HasPermission(apiKey.Scopes, resource, c.Request.Method) {
			// Get the required action for this method
			action := methodToAction[c.Request.Method]
			abortWithCode(c, apierror.ErrAuthInsufficientPermissions, "Insufficient permissions for "+string(resource)+":"+string(action))
			return
		}

		// For POST requests, inject the API key ID into the metadata
		if c.Request.Method == "POST" && c.Request.Body != nil {
			if err := injectAPIKeyToMetadata(c, apiKey.APIKeyID); err != nil {
				var sizeErr *http.MaxBytesError
				if errors.As(err, &sizeErr) {
					abortWithCode(c, apierror.ErrGenPayloadTooLarge, "request body too large")
				} else {
					abortWithCode(c, apierror.ErrGenMalformedRequest, "invalid JSON request body")
				}
				return
			}
		}

		c.Set("apiKey", apiKey)
		c.Request = c.Request.WithContext(policy.WithPrincipal(c.Request.Context(), policy.Principal{APIKeyID: apiKey.APIKeyID, Scopes: apiKey.Scopes}))

		// Capture the request context before Gin reuses its pooled context.
		requestContext, apiKeyID := c.Request.Context(), apiKey.APIKeyID
		go func() {
			_ = m.service.UpdateLastUsed(requestContext, apiKeyID)
		}()
		c.Next()
	}
}

// extractKey retrieves the authentication key from the X-LedgerForge-Key header.
//
// Parameters:
// - c: The Gin context containing the request headers.
//
// Returns:
// - string: The authentication key, or empty string if not found.
func extractKey(c *gin.Context) string {
	return c.GetHeader(KeyHeader)
}
