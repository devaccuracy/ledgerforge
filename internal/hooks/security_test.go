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

package hooks

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/devaccuracy/ledgerforge/config"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func securityManager(t *testing.T) *redisHookManager {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return &redisHookManager{client: client, config: &config.Configuration{Notification: config.Notification{Webhook: config.WebhookConfig{SigningSecret: "dedicated"}}, Server: config.ServerConfig{SecretKey: "legacy"}}}
}

func TestHookSignaturesAndRetryStatus(t *testing.T) {
	for _, status := range []int{200, 204, 302, 400, 500} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			manager := securityManager(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				ts := r.Header.Get("X-LedgerForge-Timestamp")
				stamp, err := strconv.ParseInt(ts, 10, 64)
				require.NoError(t, err)
				require.LessOrEqual(t, time.Now().Unix()-stamp, int64(5))
				mac := hmac.New(sha256.New, []byte("dedicated"))
				_, _ = mac.Write([]byte(ts + "."))
				_, _ = mac.Write(body)
				require.Equal(t, hex.EncodeToString(mac.Sum(nil)), r.Header.Get("X-LedgerForge-Signature"))
				w.WriteHeader(status)
				if status != 204 {
					_, _ = w.Write([]byte(`{"success":true}`))
				}
			}))
			defer server.Close()
			hook := &Hook{ID: "hook", URL: server.URL, Type: PreTransaction}
			err := manager.ExecuteHook(context.Background(), hook, HookPayload{TransactionID: "txn"})
			if status >= 200 && status < 300 {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestHookURLsValidatedOnCreateAndUpdate(t *testing.T) {
	m := securityManager(t)
	original := &Hook{ID: "id", URL: "https://example.com/hook", Type: PreTransaction}
	require.NoError(t, m.RegisterHook(context.Background(), original))
	for _, target := range []string{"file:///etc/passwd", "/relative", "ftp://example.com", "https://user:password@example.com", "http://example.com/#fragment", "http://example.com:99999"} {
		hook := &Hook{URL: target, Type: PreTransaction}
		require.Error(t, m.RegisterHook(context.Background(), hook))
		require.Error(t, m.UpdateHook(context.Background(), "id", hook))
	}
	got, err := m.GetHook(context.Background(), "id")
	require.NoError(t, err)
	require.Equal(t, original.URL, got.URL)
	m.config.Notification.Webhook.BlockPrivateDestinations = true
	private := &Hook{URL: "http://127.0.0.1/hook", Type: PreTransaction}
	require.Error(t, m.RegisterHook(context.Background(), private))
	require.Error(t, m.UpdateHook(context.Background(), "id", private))
	require.Error(t, m.ExecuteHook(context.Background(), private, HookPayload{}))
}

func TestWebhookAddressRestrictions(t *testing.T) {
	for _, address := range []string{"127.0.0.1", "10.1.2.3", "172.16.1.1", "192.168.1.2", "169.254.169.254", "::1", "fe80::1", "fc00::1", "::ffff:127.0.0.1", "0.0.0.0", "224.0.0.1"} {
		require.True(t, blockedAddress(netip.MustParseAddr(address)), address)
	}
	require.False(t, blockedAddress(netip.MustParseAddr("8.8.8.8")))
	require.NoError(t, ValidateURL(context.Background(), "http://127.0.0.1/hook", false))
	require.Error(t, ValidateURL(context.Background(), "http://127.0.0.1/hook", true))
}

func TestWebhookRedirectAndResponseLimits(t *testing.T) {
	m := securityManager(t)
	targetCalled := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalled = true }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer source.Close()
	require.Error(t, m.ExecuteHook(context.Background(), &Hook{ID: "id", URL: source.URL, Type: PreTransaction}, HookPayload{}))
	require.False(t, targetCalled)
	large := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", MaxResponseBytes+1)))
	}))
	defer large.Close()
	require.ErrorContains(t, m.ExecuteHook(context.Background(), &Hook{ID: "id", URL: large.URL, Type: PreTransaction}, HookPayload{}), "response too large")
	client := HTTPClient(nil, 99999, false)
	require.Equal(t, 120*time.Second, client.Timeout)
	require.Equal(t, 30*time.Second, HTTPClient(nil, 0, false).Timeout)
}

func TestSignRequestEmptySecretRemovesReservedHeaders(t *testing.T) {
	req := httptest.NewRequest("POST", "http://example.com", nil)
	req.Header.Set("X-LedgerForge-Signature", "attacker")
	req.Header.Set("X-LedgerForge-Timestamp", "attacker")
	SignRequest(req, []byte(`{}`), "")
	require.Empty(t, req.Header.Get("X-LedgerForge-Signature"))
	require.Empty(t, req.Header.Get("X-LedgerForge-Timestamp"))
}
