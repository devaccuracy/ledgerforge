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
	"errors"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	envDSN       = "LEDGERFORGE_MONITORING_DSN"
	envLegacyDSN = "LEDGERFORGE_CLOUD_DSN"

	defaultTimeout = 5 * time.Second
)

// Config describes the optional remote monitoring telemetry sink.
type Config struct {
	DSN       string
	PublicKey string
	ProjectID string
	Endpoint  string
	Timeout   time.Duration
}

// FromEnv returns the monitoring exporter config from environment variables.
// Missing DSN is not an error; it means remote export is disabled.
func FromEnv() (Config, bool, error) {
	dsn := os.Getenv(envDSN)
	if strings.TrimSpace(dsn) == "" {
		dsn = os.Getenv(envLegacyDSN)
	}
	return FromDSN(dsn)
}

func FromDSN(dsn string) (Config, bool, error) {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return Config{}, false, nil
	}

	cfg, err := ParseDSN(dsn)
	if err != nil {
		return Config{}, false, err
	}

	cfg.Timeout = defaultTimeout
	return cfg, true, nil
}

// ParseDSN parses a remote monitoring DSN:
// https://<write-key>@<host>/<project-id>
func ParseDSN(raw string) (Config, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Config{}, errors.New("monitoring exporter DSN is empty")
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return Config{}, errors.New("monitoring exporter DSN is invalid")
	}
	if parsed.Scheme != "https" {
		return Config{}, errors.New("monitoring exporter DSN must use https")
	}
	if parsed.Host == "" {
		return Config{}, errors.New("monitoring exporter DSN host is required")
	}
	if parsed.User == nil || parsed.User.Username() == "" {
		return Config{}, errors.New("monitoring exporter DSN write key is required")
	}

	projectID := strings.Trim(strings.TrimSpace(parsed.Path), "/")
	if projectID == "" {
		return Config{}, errors.New("monitoring exporter DSN project id is required")
	}
	if strings.Contains(projectID, "/") {
		return Config{}, errors.New("monitoring exporter DSN project id must be a single path segment")
	}

	endpoint := (&url.URL{
		Scheme: parsed.Scheme,
		Host:   parsed.Host,
	}).String()

	return Config{
		DSN:       raw,
		PublicKey: parsed.User.Username(),
		ProjectID: projectID,
		Endpoint:  endpoint,
		Timeout:   defaultTimeout,
	}, nil
}

func (c Config) Headers() map[string]string {
	headers := map[string]string{}
	if c.PublicKey != "" {
		headers["Authorization"] = "Bearer " + c.PublicKey
	}
	return headers
}

func (c Config) SignalURL(signal string) string {
	endpoint := strings.TrimRight(c.Endpoint, "/")
	return endpoint + "/monitoring/ingest/v1/" + strings.TrimLeft(signal, "/")
}

func (c Config) OTLPSignalURL(signal string) string {
	endpoint := strings.TrimRight(c.Endpoint, "/")
	return endpoint + "/monitoring/ingest/v1/otlp/" + strings.TrimLeft(signal, "/")
}
