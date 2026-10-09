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
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devaccuracy/ledgerforge/config"
	"github.com/devaccuracy/ledgerforge/database/mocks"
	"github.com/devaccuracy/ledgerforge/internal/notification"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestCloseReleasesAllOwnedResourcesAfterAnError(t *testing.T) {
	cfg := &config.Configuration{Redis: config.RedisConfig{Dns: testRedisAddr()}}
	config.ConfigStore.Store(cfg)
	ledger, err := NewLedgerForge(new(mocks.MockDataSource))
	require.NoError(t, err)
	require.NoError(t, ledger.redis.Ping(context.Background()).Err())
	transport := &resourceTransport{RoundTripper: ledger.httpClient.Transport}
	ledger.httpClient.Transport = transport
	// A failed close of one resource must not skip the other resources.
	require.NoError(t, ledger.asynqClient.Close())
	var workers sync.WaitGroup
	failures := make(chan error, 8)
	for range 8 {
		workers.Add(1)
		go func() { defer workers.Done(); failures <- ledger.Close() }()
	}
	workers.Wait()
	close(failures)
	for err := range failures {
		require.Error(t, err)
	}
	require.Equal(t, int64(1), transport.closed.Load())
	require.ErrorIs(t, ledger.redis.Ping(context.Background()).Err(), redis.ErrClosed)
	_, err = ledger.queue.Inspector.Queues()
	require.ErrorContains(t, err, "closed")
	_, err = ledger.StartReindex(1)
	require.Error(t, err)
}

type resourceTransport struct {
	http.RoundTripper
	closed atomic.Int64
}

func (t *resourceTransport) CloseIdleConnections() { t.closed.Add(1) }

func TestInstanceErrorNotificationsUseOwnQueue(t *testing.T) {
	legacy := make(chan struct{}, 1)
	notification.RegisterWebhookSender(func(string, interface{}) error { legacy <- struct{}{}; return nil })
	defer notification.RegisterWebhookSender(nil)
	create := func(name string) *LedgerForge {
		cfg := &config.Configuration{
			Redis:        config.RedisConfig{Dns: testRedisAddr()},
			Queue:        config.QueueConfig{WebhookQueue: name},
			Notification: config.Notification{Webhook: config.WebhookConfig{Url: "http://example.invalid"}},
		}
		config.ConfigStore.Store(cfg)
		instance, err := NewLedgerForge(new(mocks.MockDataSource))
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, instance.Close()) })
		return instance
	}
	first := create("instance_error_first")
	second := create("instance_error_second")
	// Constructors must leave explicit legacy registration intact.
	notification.NotifyError(errors.New("legacy"))
	select {
	case <-legacy:
	case <-time.After(time.Second):
		t.Fatal("constructor replaced legacy error sender")
	}
	first.notifyError(errors.New("first"))
	second.notifyError(errors.New("second"))
	for _, instance := range []struct {
		ledger *LedgerForge
		want   string
	}{{first, "first"}, {second, "second"}} {
		require.Eventually(t, func() bool {
			tasks, err := instance.ledger.queue.Inspector.ListPendingTasks(instance.ledger.Config().Queue.WebhookQueue)
			return err == nil && len(tasks) == 1
		}, time.Second, 10*time.Millisecond)
		tasks, err := instance.ledger.queue.Inspector.ListPendingTasks(instance.ledger.Config().Queue.WebhookQueue)
		require.NoError(t, err)
		var payload NewWebhook
		require.NoError(t, json.Unmarshal(tasks[0].Payload, &payload))
		require.Equal(t, "system.error", payload.Event)
		require.Equal(t, instance.want, payload.Payload.(map[string]interface{})["error"])
	}
	require.Empty(t, legacy, "instance notifications must not use the legacy sender")
}
