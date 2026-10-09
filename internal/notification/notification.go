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

package notification

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/devaccuracy/ledgerforge/internal/request"

	"github.com/devaccuracy/ledgerforge/config"
	"github.com/sirupsen/logrus"
)

// SlackNotification sends an error message to a Slack webhook.
// It formats the error details and the current time into a Slack message payload.
//
// Parameters:
// - err: The error to be reported via Slack.
//
// The function retrieves configuration for the Slack webhook URL, formats the error,
// and sends it as a JSON payload to the Slack webhook.
func SlackNotification(err error) {
	conf, fetchErr := config.Fetch()
	if fetchErr != nil {
		logrus.Error(fetchErr)
		return
	}
	slackNotification(err, conf)
}

func slackNotification(err error, conf *config.Configuration) {
	// Build the Slack message payload with typed structs and json.Marshal so
	// error text containing quotes, backslashes, or newlines is safely
	// encoded instead of corrupting (or injecting into) the JSON template.
	type slackText struct {
		Type  string `json:"type"`
		Text  string `json:"text"`
		Emoji bool   `json:"emoji,omitempty"`
	}
	type slackBlock struct {
		Type   string      `json:"type"`
		Text   *slackText  `json:"text,omitempty"`
		Fields []slackText `json:"fields,omitempty"`
	}
	payloadStruct := struct {
		Blocks []slackBlock `json:"blocks"`
	}{
		Blocks: []slackBlock{
			{
				Type: "header",
				Text: &slackText{Type: "plain_text", Text: "Error From LedgerForge 🐞", Emoji: true},
			},
			{
				Type:   "section",
				Fields: []slackText{{Type: "mrkdwn", Text: fmt.Sprintf("*Error:*\n%v", err.Error())}},
			},
			{
				Type:   "section",
				Fields: []slackText{{Type: "mrkdwn", Text: fmt.Sprintf("*Time:*\n%v", time.Now().Format(time.RFC822))}},
			},
		},
	}

	encoded, err := json.Marshal(payloadStruct)
	if err != nil {
		logrus.Error(err)
		return
	}
	data := json.RawMessage(encoded)

	// Convert the Slack message to a JSON request payload
	payload, err := request.ToJsonReq(&data)
	if err != nil {
		logrus.Error(err)
		return
	}

	// Create an HTTP request to send the Slack notification
	req, err := http.NewRequest("POST", conf.Notification.Slack.WebhookUrl, payload)
	if err != nil {
		logrus.Error(err)
		return
	}

	// Send the request and handle the response
	var response map[string]interface{}
	_, err = request.Call(req, &response)
	if err != nil {
		logrus.Error(err)
	}
}

// WebhookSender defines a function signature for sending webhooks.
type WebhookSender func(event string, payload interface{}) error

var (
	webhookSenderMu sync.RWMutex
	webhookSender   WebhookSender
)

// RegisterWebhookSender registers a function to handle webhook sending.
func RegisterWebhookSender(sender WebhookSender) {
	webhookSenderMu.Lock()
	webhookSender = sender
	webhookSenderMu.Unlock()
}

// getWebhookSender returns the currently registered webhook sender, if any.
func getWebhookSender() WebhookSender {
	webhookSenderMu.RLock()
	defer webhookSenderMu.RUnlock()
	return webhookSender
}

// NotifyError sends an error notification through the configured notification system.
// It logs the error locally and sends a notification via Slack (if configured).
//
// Parameters:
// - systemError: The error to notify.
//
// This function runs the notification process asynchronously using a goroutine to avoid blocking.
func NotifyError(systemError error) {
	go notifyError(systemError, config.Fetch, getWebhookSender)
}

// NotifyErrorWithSender delivers errors using the originating instance's configuration and sender.
func NotifyErrorWithSender(systemError error, conf *config.Configuration, sender WebhookSender) {
	go notifyError(systemError, func() (*config.Configuration, error) { return conf, nil }, func() WebhookSender { return sender })
}

func notifyError(systemError error, configuration func() (*config.Configuration, error), resolveSender func() WebhookSender) {
	logrus.Error(systemError)
	conf, err := configuration()
	if err != nil {
		logrus.Error(err)
		return
	}
	if conf.Notification.Slack.WebhookUrl != "" {
		slackNotification(systemError, conf)
	}
	sender := resolveSender()
	if sender != nil && conf.Notification.Webhook.Url != "" {
		payload := map[string]interface{}{"error": systemError.Error(), "time": time.Now()}
		if err := sender("system.error", payload); err != nil {
			logrus.Errorf("Error sending webhook notification: %v", err)
		}
	}
}
