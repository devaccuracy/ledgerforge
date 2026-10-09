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
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// NewTraceExporter creates an OTLP/HTTP trace exporter for remote monitoring.
func NewTraceExporter(ctx context.Context, cfg Config) (sdktrace.SpanExporter, error) {
	return otlptracehttp.New(ctx,
		otlptracehttp.WithEndpointURL(cfg.OTLPSignalURL("traces")),
		otlptracehttp.WithHeaders(cfg.Headers()),
		otlptracehttp.WithTimeout(cfg.Timeout),
	)
}

// NewMetricReader creates an OTLP/HTTP metric reader for remote monitoring.
func NewMetricReader(ctx context.Context, cfg Config) (sdkmetric.Reader, error) {
	exporter, err := otlpmetrichttp.New(ctx,
		otlpmetrichttp.WithEndpointURL(cfg.OTLPSignalURL("metrics")),
		otlpmetrichttp.WithHeaders(cfg.Headers()),
		otlpmetrichttp.WithTimeout(cfg.Timeout),
	)
	if err != nil {
		return nil, err
	}

	return sdkmetric.NewPeriodicReader(exporter,
		sdkmetric.WithInterval(60*time.Second),
		sdkmetric.WithTimeout(cfg.Timeout),
	), nil
}
