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
	"sync"
	"time"
)

var (
	ErrReindexInProgress = errors.New("a reindex operation is already in progress")
	ErrReindexNotStarted = errors.New("no reindex operation has been started")
	ErrReindexClosed     = errors.New("reindex service is closed")
)

// ReindexJobs coordinates background jobs for one ledger instance.
type ReindexJobs struct {
	mu            sync.Mutex
	client        *TypesenseClient
	repository    ReindexRepository
	retryInterval time.Duration
	service       *ReindexService
	active        bool
	closed        bool
	cancel        context.CancelFunc
	workers       sync.WaitGroup
}

func NewReindexJobs(client *TypesenseClient, repository ReindexRepository, retryInterval time.Duration) *ReindexJobs {
	return &ReindexJobs{client: client, repository: repository, retryInterval: retryInterval}
}

func (j *ReindexJobs) Start(batchSize int) (ReindexProgress, error) {
	return j.start(batchSize, 0)
}

func (j *ReindexJobs) start(batchSize int, timeout time.Duration) (ReindexProgress, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return ReindexProgress{}, ErrReindexClosed
	}
	if j.active {
		return j.service.GetProgress(), ErrReindexInProgress
	}
	service := NewReindexService(j.client, j.repository, ReindexConfig{BatchSize: batchSize, BackpressureRetryInterval: j.retryInterval})
	service.progress = &ReindexProgress{Status: "in_progress", Phase: "starting", StartedAt: time.Now()}
	ctx, cancel := context.WithCancel(context.Background())
	if timeout > 0 {
		cancel()
		ctx, cancel = context.WithTimeout(context.Background(), timeout)
	}
	j.cancel = cancel
	j.service = service
	j.active = true
	j.workers.Add(1)
	go func() {
		defer j.workers.Done()
		defer cancel()
		_, _ = service.StartReindex(ctx)
		j.mu.Lock()
		j.active = false
		j.mu.Unlock()
	}()
	return service.GetProgress(), nil
}

func (j *ReindexJobs) Progress() (ReindexProgress, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.service == nil {
		return ReindexProgress{}, ErrReindexNotStarted
	}
	return j.service.GetProgress(), nil
}

func (j *ReindexJobs) Close() {
	j.mu.Lock()
	j.closed = true
	if j.cancel != nil {
		j.cancel()
	}
	j.mu.Unlock()
	j.workers.Wait()
}

// TryReindexIfNeeded uses the same admission and progress state as manual jobs.
func (j *ReindexJobs) TryReindexIfNeeded(ctx context.Context) {
	if j.client == nil || j.repository == nil || !shouldReindex(ctx, j.client, j.repository) {
		return
	}
	_, _ = j.start(1000, 30*time.Minute)
}
