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
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/devaccuracy/ledgerforge/config"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

func TestInstanceErrorNotificationsDeliverWithOwnConfiguration(t *testing.T) {
	for _, disableGlobal := range []bool{false, true} {
		name := "latest_instance_has_another_receiver"
		if disableGlobal {
			name = "global_webhook_disabled"
		}
		t.Run(name, func(t *testing.T) {
			firstReceiver, firstRequests := newWebhookReceiver(http.StatusNoContent)
			t.Cleanup(firstReceiver.Close)
			secondReceiver, secondRequests := newWebhookReceiver(http.StatusNoContent)
			t.Cleanup(secondReceiver.Close)
			create := func(url, tenant, secret string) *LedgerForge {
				cfg, _ := storeWebhookTestConfig(url, "master-"+tenant, map[string]string{"X-Tenant": tenant})
				cfg.Notification.Webhook.SigningSecret = secret
				instance, err := NewLedgerForge(nil)
				require.NoError(t, err)
				t.Cleanup(func() { require.NoError(t, instance.Close()) })
				return instance
			}
			first := create(firstReceiver.URL, "first", "first-signing-secret")
			second := create(secondReceiver.URL, "second", "second-signing-secret")
			// Enqueue through scoped error notifications after the second constructor replaced global config.
			first.notifyError(errors.New("first error"))
			second.notifyError(errors.New("second error"))
			if disableGlobal {
				config.ConfigStore.Store(&config.Configuration{})
			}

			// Read the actual Redis task payloads and execute the corresponding instance's worker.
			results := make(chan error, 2)
			for _, instance := range []*LedgerForge{first, second} {
				queue := instance.Config().Queue.WebhookQueue
				require.Eventually(t, func() bool {
					tasks, err := instance.queue.Inspector.ListPendingTasks(queue)
					return err == nil && len(tasks) == 1
				}, time.Second, 10*time.Millisecond)
				tasks, err := instance.queue.Inspector.ListPendingTasks(queue)
				require.NoError(t, err)
				require.Len(t, tasks, 1)
				task := asynq.NewTask(tasks[0].Type, tasks[0].Payload)
				go func() { results <- instance.ProcessWebhook(context.Background(), task) }()
			}
			for range 2 {
				select {
				case err := <-results:
					require.NoError(t, err)
				case <-time.After(5 * time.Second):
					t.Fatal("webhook delivery did not complete")
				}
			}
			for _, receiver := range []struct {
				requests func() []receivedWebhook
				tenant   string
				secret   string
			}{{firstRequests, "first", "first-signing-secret"}, {secondRequests, "second", "second-signing-secret"}} {
				requests := receiver.requests()
				require.Len(t, requests, 1, "each receiver must get only its own instance's task")
				request := requests[0]
				require.Equal(t, receiver.tenant, request.headers.Get("X-Tenant"))
				var event NewWebhook
				require.NoError(t, json.Unmarshal(request.body, &event))
				require.Equal(t, "system.error", event.Event)
				require.Equal(t, receiver.tenant+" error", event.Payload.(map[string]interface{})["error"])
				timestamp := request.headers.Get("X-LedgerForge-Timestamp")
				require.NotEmpty(t, timestamp)
				mac := hmac.New(sha256.New, []byte(receiver.secret))
				_, _ = mac.Write([]byte(timestamp + "."))
				_, _ = mac.Write(request.body)
				require.Equal(t, hex.EncodeToString(mac.Sum(nil)), request.headers.Get("X-LedgerForge-Signature"))
			}
		})
	}
}
