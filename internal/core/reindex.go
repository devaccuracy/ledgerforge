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

	"github.com/devaccuracy/ledgerforge/internal/search"
)

func (b *LedgerForge) reindexJobs() *search.ReindexJobs {
	b.reindexOnce.Do(func() {
		b.reindex = search.NewReindexJobs(b.search, b.datasource, b.Config().TypeSense.BackpressureRetryInterval)
	})
	return b.reindex
}

func (b *LedgerForge) StartReindex(batchSize int) (search.ReindexProgress, error) {
	return b.reindexJobs().Start(batchSize)
}

func (b *LedgerForge) GetReindexProgress() (search.ReindexProgress, error) {
	return b.reindexJobs().Progress()
}

// TryReindexIfNeeded starts a rebuild on an empty search index using this instance's job coordinator.
func (b *LedgerForge) TryReindexIfNeeded(ctx context.Context) {
	b.reindexJobs().TryReindexIfNeeded(ctx)
}
