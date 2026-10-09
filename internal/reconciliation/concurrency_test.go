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

package reconciliation

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devaccuracy/ledgerforge/database"
	"github.com/devaccuracy/ledgerforge/model"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

type checkpointRepository struct {
	database.Datasource
	entered chan struct{}
	release chan struct{}
	active  atomic.Int64
	peak    atomic.Int64
	mu      sync.Mutex
	saved   []int
}

func (r *checkpointRepository) SaveReconciliationProgress(ctx context.Context, id string, progress model.ReconciliationProgress) error {
	current := r.active.Add(1)
	defer r.active.Add(-1)
	for old := r.peak.Load(); current > old; old = r.peak.Load() {
		if r.peak.CompareAndSwap(old, current) {
			break
		}
	}
	if progress.ProcessedCount == 1 {
		close(r.entered)
		<-r.release
	}
	err := r.Datasource.SaveReconciliationProgress(ctx, id, progress)
	r.mu.Lock()
	r.saved = append(r.saved, progress.ProcessedCount)
	r.mu.Unlock()
	return err
}

func TestProcessorConcurrentCheckpointsPostgres(t *testing.T) {
	if testing.Short() {
		t.Skip("requires isolated PostgreSQL via itest.sh")
	}
	db, err := sql.Open("postgres", "postgres://postgres:password@localhost:5432/ledgerforge?sslmode=disable")
	require.NoError(t, err)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	require.NoError(t, db.PingContext(ctx))
	id := model.GenerateUUIDWithSuffix("recon")
	repository := &checkpointRepository{Datasource: database.Datasource{Conn: db}, entered: make(chan struct{}), release: make(chan struct{})}
	defer func() {
		select {
		case <-repository.release:
		default:
			close(repository.release)
		}
	}()
	rec := model.Reconciliation{ReconciliationID: id, UploadID: id, Status: StatusStarted, StartedAt: time.Now()}
	require.NoError(t, repository.RecordReconciliation(ctx, &rec))
	defer func() {
		for _, table := range []string{"matches", "unmatched", "reconciliation_progress", "reconciliations"} {
			_, _ = db.Exec("DELETE FROM ledgerforge."+table+" WHERE reconciliation_id = $1", id)
		}
	}()
	const count = 32
	metadataDone := make(chan struct{}, count)
	service := NewService(repository, func() Config { return Config{MaxWorkers: 8, ProgressInterval: 1} },
		func(ctx context.Context, _ string, _ *big.Int, _ int, _ FetchTransactions, process ProcessTransaction) error {
			var workers sync.WaitGroup
			failures := make(chan error, count)
			// Delay the oldest checkpoint while all other workers attempt to save newer ones.
			workers.Add(1)
			go func() { defer workers.Done(); failures <- process(ctx, &model.Transaction{TransactionID: "ext-first"}) }()
			select {
			case <-repository.entered:
			case <-ctx.Done():
				return ctx.Err()
			}
			for i := 1; i < count; i++ {
				workers.Add(1)
				go func(i int) {
					defer workers.Done()
					failures <- process(ctx, &model.Transaction{TransactionID: fmt.Sprintf("ext-%d", i)})
				}(i)
			}
			time.Sleep(50 * time.Millisecond)
			close(repository.release)
			workers.Wait()
			close(failures)
			for err := range failures {
				if err != nil {
					return err
				}
			}
			return nil
		}, func(context.Context, string, map[string]interface{}) error { metadataDone <- struct{}{}; return nil }, func(model.Reconciliation) {})
	processor := service.createTransactionProcessor(rec, model.ReconciliationProgress{}, func(_ context.Context, txns []*model.Transaction) ([]model.Match, []string) {
		id := txns[0].TransactionID
		return []model.Match{{ExternalTransactionID: id, InternalTransactionID: "internal-" + id, Amount: 10, Date: time.Now()}}, []string{id}
	})
	require.NoError(t, service.processTransactions(ctx, id, processor, "one_to_one"))
	matched, unmatched := processor.getResults()
	require.Equal(t, count, matched)
	require.Equal(t, count, unmatched)
	progress, err := repository.LoadReconciliationProgress(ctx, id)
	require.NoError(t, err)
	require.Equal(t, count, progress.ProcessedCount)
	require.NotEmpty(t, progress.LastProcessedExternalTxnID)
	require.Equal(t, int64(1), repository.peak.Load(), "checkpoint writes must be serialized")
	repository.mu.Lock()
	saved := append([]int(nil), repository.saved...)
	repository.mu.Unlock()
	require.Len(t, saved, count)
	for i, savedCount := range saved {
		require.Equal(t, i+1, savedCount, "checkpoints must never regress")
	}
	for _, table := range []string{"matches", "unmatched"} {
		var persisted int
		require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM ledgerforge."+table+" WHERE reconciliation_id = $1", id).Scan(&persisted))
		require.Equal(t, count, persisted)
	}
	for range count {
		select {
		case <-metadataDone:
		case <-ctx.Done():
			t.Fatal("metadata callbacks did not finish")
		}
	}
}

func TestMatchingConcurrentCandidatesProduceOneMatch(t *testing.T) {
	var processed atomic.Int64
	service := NewService(nil, func() Config { return Config{MaxWorkers: 16} },
		func(ctx context.Context, _ string, _ *big.Int, _ int, _ FetchTransactions, process ProcessTransaction) error {
			var workers sync.WaitGroup
			for range 64 {
				workers.Add(1)
				go func() {
					defer workers.Done()
					_ = process(ctx, &model.Transaction{Amount: 10, Currency: "USD"})
					processed.Add(1)
				}()
			}
			workers.Wait()
			return nil
		}, nil, nil)
	matches := make(chan model.Match, 64)
	unmatched := make(chan string, 64)
	err := service.findMatchingInternalTransaction(context.Background(), &model.Transaction{TransactionID: "external", Amount: 10, Currency: "USD"},
		[]model.MatchingRule{{Criteria: []model.MatchingCriteria{{Field: "amount", Operator: "equals"}}}}, matches, unmatched)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	require.Empty(t, unmatched)
	require.Equal(t, int64(64), processed.Load(), "all candidates must be drained")
}
