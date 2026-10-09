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

package monitoringexporter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func TestLogHookPostsSanitizedPayload(t *testing.T) {
	received := make(chan logPayload, 1)
	var authHeader string
	var projectHeader string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/monitoring/ingest/v1/logs" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		authHeader = r.Header.Get("Authorization")
		projectHeader = r.Header.Get("X-LedgerForge-Project-ID")

		var payload logPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("failed to decode payload: %v", err)
		}
		received <- payload
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	hook := NewLogHook(Config{
		PublicKey: "ledgerforge_obs_test",
		ProjectID: "monproj_test",
		Endpoint:  server.URL,
		Timeout:   time.Second,
	})

	err := hook.Fire(&logrus.Entry{
		Time:    time.Now(),
		Level:   logrus.ErrorLevel,
		Message: "failed request",
		Data: logrus.Fields{
			"Authorization": "Bearer secret",
			"status":        "failed",
		},
	})
	if err != nil {
		t.Fatalf("Fire returned error: %v", err)
	}

	select {
	case payload := <-received:
		if authHeader != "Bearer ledgerforge_obs_test" {
			t.Fatalf("unexpected auth header: %s", authHeader)
		}
		if projectHeader != "" {
			t.Fatalf("did not expect project header: %s", projectHeader)
		}
		if payload.Fields["Authorization"] != redacted {
			t.Fatalf("expected Authorization field to be redacted: %+v", payload.Fields)
		}
		if payload.Fields["status"] != "failed" {
			t.Fatalf("expected safe status field to remain: %+v", payload.Fields)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for log payload")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := hook.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown returned error: %v", err)
	}
}
