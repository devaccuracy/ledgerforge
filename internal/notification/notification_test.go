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
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRegisterWebhookSender(t *testing.T) {
	// Reset the global webhook sender
	RegisterWebhookSender(nil)

	// Create a mock webhook sender
	mockSender := func(event string, payload interface{}) error {
		return nil
	}

	// Register the mock sender
	RegisterWebhookSender(mockSender)

	// Verify the sender was registered
	assert.NotNil(t, registeredWebhookSender())
}

func TestRegisterWebhookSender_CalledCorrectly(t *testing.T) {
	// Reset the global webhook sender
	RegisterWebhookSender(nil)

	var capturedEvent string
	var capturedPayload interface{}

	// Create a mock webhook sender that captures arguments
	mockSender := func(event string, payload interface{}) error {
		capturedEvent = event
		capturedPayload = payload
		return nil
	}

	// Register the mock sender
	RegisterWebhookSender(mockSender)

	// Call the sender
	testPayload := map[string]string{"key": "value"}
	err := registeredWebhookSender()("test.event", testPayload)

	assert.NoError(t, err)
	assert.Equal(t, "test.event", capturedEvent)
	assert.Equal(t, testPayload, capturedPayload)
}

func TestRegisterWebhookSender_ReturnsError(t *testing.T) {
	// Reset the global webhook sender
	RegisterWebhookSender(nil)

	expectedError := errors.New("webhook failed")

	// Create a mock webhook sender that returns an error
	mockSender := func(event string, payload interface{}) error {
		return expectedError
	}

	// Register the mock sender
	RegisterWebhookSender(mockSender)

	// Call the sender
	err := registeredWebhookSender()("test.event", nil)

	assert.Error(t, err)
	assert.Equal(t, expectedError, err)
}

func TestRegisterWebhookSender_ReplacesPrevious(t *testing.T) {
	// Reset the global webhook sender
	RegisterWebhookSender(nil)

	callCount := 0

	// Register first sender
	RegisterWebhookSender(func(event string, payload interface{}) error {
		callCount = 1
		return nil
	})

	// Register second sender (should replace first)
	RegisterWebhookSender(func(event string, payload interface{}) error {
		callCount = 2
		return nil
	})

	// Call the sender
	_ = registeredWebhookSender()("test.event", nil)

	// Second sender should have been called
	assert.Equal(t, 2, callCount)
}

func TestWebhookSenderRegistrationIsConcurrentSafe(t *testing.T) {
	const iterations = 100

	var waitGroup sync.WaitGroup
	for range iterations {
		waitGroup.Add(2)

		go func() {
			defer waitGroup.Done()
			RegisterWebhookSender(func(string, interface{}) error { return nil })
		}()

		go func() {
			defer waitGroup.Done()
			_ = registeredWebhookSender()
		}()
	}

	waitGroup.Wait()
	assert.NotNil(t, registeredWebhookSender())
}
