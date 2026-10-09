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

package notification

import (
	"errors"
	"testing"
	"time"

	"github.com/devaccuracy/ledgerforge/config"
	"github.com/stretchr/testify/require"
)

func TestNotifyErrorWithSenderKeepsOriginatingInstance(t *testing.T) {
	previous := getWebhookSender()
	defer RegisterWebhookSender(previous)
	wrong := make(chan struct{}, 1)
	RegisterWebhookSender(func(string, interface{}) error { wrong <- struct{}{}; return nil })
	first := make(chan string, 1)
	second := make(chan string, 1)
	cfg := &config.Configuration{Notification: config.Notification{Webhook: config.WebhookConfig{Url: "http://example.invalid"}}}
	sender := func(calls chan string) WebhookSender {
		return func(event string, payload interface{}) error {
			if event != "system.error" {
				t.Errorf("unexpected event %s", event)
			}
			calls <- payload.(map[string]interface{})["error"].(string)
			return nil
		}
	}
	NotifyErrorWithSender(errors.New("first"), cfg, sender(first))
	NotifyErrorWithSender(errors.New("second"), cfg, sender(second))
	for _, want := range []struct {
		calls   <-chan string
		message string
	}{{first, "first"}, {second, "second"}} {
		select {
		case got := <-want.calls:
			require.Equal(t, want.message, got)
		case <-time.After(time.Second):
			t.Fatal("scoped error was not delivered")
		}
	}
	require.Empty(t, wrong, "scoped notifications must not consult the global sender")
}
