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

package core

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/devaccuracy/ledgerforge/config"
	"github.com/stretchr/testify/require"
)

func TestNotificationDedicatedSigningSecretAndReservedHeaders(t *testing.T) {
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		ts := r.Header.Get("X-LedgerForge-Timestamp")
		require.NotEqual(t, "attacker", ts)
		mac := hmac.New(sha256.New, []byte("dedicated"))
		_, _ = mac.Write([]byte(ts + "."))
		_, _ = mac.Write(body)
		require.Equal(t, hex.EncodeToString(mac.Sum(nil)), r.Header.Get("X-LedgerForge-Signature"))
		require.Equal(t, "tenant", r.Header.Get("X-Tenant"))
		w.WriteHeader(204)
	}))
	defer receiver.Close()
	config.ConfigStore.Store(&config.Configuration{Server: config.ServerConfig{SecretKey: "master"}, Notification: config.Notification{Webhook: config.WebhookConfig{Url: receiver.URL, SigningSecret: "dedicated", Headers: map[string]string{"x-ledgerforge-signature": "attacker", "x-ledgerforge-timestamp": "attacker", "X-Tenant": "tenant"}}}})
	require.NoError(t, processHTTP(NewWebhook{Event: "transaction.applied"}, receiver.Client()))
}

func TestNotificationDestinationRestrictionsAndRedirects(t *testing.T) {
	called := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer source.Close()
	conf := &config.Configuration{Notification: config.Notification{Webhook: config.WebhookConfig{Url: source.URL, SigningSecret: "secret"}}}
	config.ConfigStore.Store(conf)
	require.Error(t, processHTTP(NewWebhook{}, source.Client()))
	require.False(t, called)
	conf.Notification.Webhook.Url = target.URL
	conf.Notification.Webhook.BlockPrivateDestinations = true
	require.Error(t, processHTTP(NewWebhook{}, target.Client()))
	require.False(t, called)
}
