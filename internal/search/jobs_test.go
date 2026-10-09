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

package search

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devaccuracy/ledgerforge/database/mocks"
	"github.com/devaccuracy/ledgerforge/model"
	"github.com/stretchr/testify/require"
)

func TestReindexJobsConcurrentAdmissionAndInstanceIsolation(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	defer close(release)
	client := newFaultClient(server.URL, 10*time.Second)
	first := NewReindexJobs(client, nil, 0)
	second := NewReindexJobs(client, nil, 0)
	defer first.Close()
	defer second.Close()
	_, err := first.Progress()
	require.ErrorIs(t, err, ErrReindexNotStarted)
	var accepted atomic.Int64
	var unexpected atomic.Int64
	var workers sync.WaitGroup
	for range 32 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			progress, err := first.Start(17)
			if err == nil {
				accepted.Add(1)
			} else if !errors.Is(err, ErrReindexInProgress) {
				unexpected.Add(1)
			}
			if progress.Status != "in_progress" {
				unexpected.Add(1)
			}
		}()
	}
	workers.Wait()
	require.Equal(t, int64(1), accepted.Load())
	require.Zero(t, unexpected.Load())
	// Another instance has independent progress and admission even while the first is busy.
	_, err = second.Progress()
	require.ErrorIs(t, err, ErrReindexNotStarted)
	progress, err := second.Start(23)
	require.NoError(t, err)
	require.Equal(t, "in_progress", progress.Status)
	require.Equal(t, 17, first.service.config.BatchSize)
	require.Equal(t, 23, second.service.config.BatchSize)
}

func TestReindexJobsCloseCancelsAndJoinsJob(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case entered <- struct{}{}:
		default:
		}
		select {
		case <-release:
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	defer close(release)
	jobs := NewReindexJobs(newFaultClient(server.URL, 10*time.Second), nil, 0)
	defer jobs.Close()
	_, err := jobs.Start(1)
	require.NoError(t, err)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("job never reached search server")
	}
	done := make(chan struct{})
	go func() { jobs.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not cancel and join reindex")
	}
	progress, err := jobs.Progress()
	require.NoError(t, err)
	require.Equal(t, "failed", progress.Status)
	_, err = jobs.Start(1)
	require.ErrorIs(t, err, ErrReindexClosed)
	jobs.Close()
}

func TestAutomaticReindexSharesManualAdmission(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"transactions","num_documents":0}`))
			return
		}
		select {
		case <-release:
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	defer close(release)
	repository := new(mocks.MockDataSource)
	repository.On("GetAllLedgers", 2, 0).Return([]model.Ledger{{LedgerID: "general"}, {LedgerID: "other"}}, nil).Once()
	jobs := NewReindexJobs(newFaultClient(server.URL, 10*time.Second), repository, 0)
	defer jobs.Close()
	_, err := jobs.Start(17)
	require.NoError(t, err)
	original := jobs.service
	jobs.TryReindexIfNeeded(context.Background())
	require.Same(t, original, jobs.service, "startup must not replace an admitted manual job")
	require.Equal(t, 17, jobs.service.config.BatchSize)
	repository.AssertExpectations(t)
}
