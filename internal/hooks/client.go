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

package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

// ExecuteHook is a public wrapper for processing a hook task.
// It executes the webhook by sending an HTTP POST request to the configured URL.
//
// Parameters:
// - ctx: The context for the operation.
// - hook: The hook configuration containing URL and other settings.
// - payload: The payload to send in the webhook request.
//
// Returns:
// - error: An error if the webhook execution fails.
func (m *redisHookManager) ExecuteHook(ctx context.Context, hook *Hook, payload HookPayload) error {
	return m.executeHook(ctx, hook, payload)
}

// executeHook performs the actual HTTP request for the webhook.
// It handles marshalling the payload, creating the request, and processing the response.
// Retries are handled by the queue system, so this function only performs a single attempt.
func (m *redisHookManager) executeHook(ctx context.Context, hook *Hook, payload HookPayload) error {
	if m.config == nil {
		return fmt.Errorf("config is not initialized")
	}
	if err := validateHook(hook); err != nil {
		return err
	}
	client := HTTPClient(nil, hook.Timeout, m.config.Notification.Webhook.BlockPrivateDestinations)

	// Marshal payload with explicit handling
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Validate JSON before sending
	if !json.Valid(payloadBytes) {
		return fmt.Errorf("invalid JSON payload generated")
	}

	logrus.WithFields(logrus.Fields{
		"hook_id":   hook.ID,
		"hook_name": hook.Name,
		"hook_url":  SafeURL(hook.URL),
		"hook_type": hook.Type,
	}).Info("Executing webhook")

	req, err := http.NewRequestWithContext(ctx, "POST", hook.URL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	secret := m.config.WebhookSigningSecret()
	SignRequest(req, payloadBytes, secret)
	if secret == "" {
		logrus.Warn("webhook sent unsigned: signing secret is not configured")
	}
	req.Header.Set("X-Hook-ID", hook.ID)
	req.Header.Set("X-Hook-Type", string(hook.Type))

	resp, err := client.Do(req)
	if err != nil {
		// Update hook execution status on failure
		_ = m.updateHookStatus(ctx, hook, false)
		if ctx.Err() != nil {
			logrus.WithFields(logrus.Fields{
				"hook_id":   hook.ID,
				"hook_type": hook.Type,
				"error":     ctx.Err(),
			}).Error("Hook execution cancelled due to context timeout")
			return ctx.Err()
		}
		logrus.WithField("hook_id", hook.ID).Warn("webhook transport failed")
		return fmt.Errorf("webhook transport failed")
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			logrus.WithError(err).Error("Failed to close response body")
		}
	}()

	// Read response body
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil {
		_ = m.updateHookStatus(ctx, hook, false)
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if len(body) > MaxResponseBytes {
		_ = m.updateHookStatus(ctx, hook, false)
		return fmt.Errorf("webhook response too large")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = m.updateHookStatus(ctx, hook, false)
		return fmt.Errorf("webhook delivery failed with status %d", resp.StatusCode)
	}

	// Legacy receivers may return empty or non-JSON success bodies. JSON
	// receivers can additionally reject delivery with success=false.
	if len(body) > 0 && json.Valid(body) {
		var result HookResponse
		if err := json.Unmarshal(body, &result); err == nil && !result.Success {
			_ = m.updateHookStatus(ctx, hook, false)
			return fmt.Errorf("hook receiver rejected delivery")
		}
	}
	logrus.WithFields(logrus.Fields{"hook_id": hook.ID, "status_code": resp.StatusCode}).Info("Hook executed successfully")
	_ = m.updateHookStatus(ctx, hook, true)
	return nil
}

func (m *redisHookManager) updateHookStatus(ctx context.Context, hook *Hook, success bool) error {
	hook.LastRun = time.Now()
	hook.LastSuccess = success

	logrus.WithFields(logrus.Fields{
		"hook_id":   hook.ID,
		"hook_name": hook.Name,
		"hook_type": hook.Type,
		"success":   success,
		"last_run":  hook.LastRun,
	}).Info("Updated hook execution status")

	data, err := json.Marshal(hook)
	if err != nil {
		return fmt.Errorf("failed to marshal hook: %w", err)
	}

	key := fmt.Sprintf("%s:%s", hookKeyPrefix, hook.ID)
	return m.client.Set(ctx, key, data, 0).Err()
}
