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

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func TestLogrusAccessLoggerDropsQueryString(t *testing.T) {
	var buf bytes.Buffer
	logger := logrus.StandardLogger()
	previousOutput := logger.Out
	previousFormatter := logger.Formatter
	defer func() {
		logger.SetOutput(previousOutput)
		logger.SetFormatter(previousFormatter)
	}()

	logger.SetOutput(&buf)
	logger.SetFormatter(&logrus.JSONFormatter{})

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(logrusAccessLogger())
	router.GET("/transactions", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/transactions?token=secret&limit=20", nil)
	router.ServeHTTP(httptest.NewRecorder(), req)

	var entry map[string]interface{}
	if err := json.NewDecoder(&buf).Decode(&entry); err != nil {
		t.Fatalf("failed to decode log entry: %v", err)
	}
	if entry["path"] != "/transactions" {
		t.Fatalf("expected query-free path, got %+v", entry["path"])
	}
	if bytes.Contains(buf.Bytes(), []byte("token=secret")) {
		t.Fatalf("access log leaked query string: %s", buf.String())
	}
}
