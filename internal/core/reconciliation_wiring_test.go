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
	"errors"
	"math/big"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devaccuracy/ledgerforge/config"
	"github.com/devaccuracy/ledgerforge/model"
	"github.com/stretchr/testify/require"
)

// Retain the helper for existing core tests after its implementation moved to reconciliation.
func contains(values []string, value string) bool { return slices.Contains(values, value) }

func TestReconciliationBatchAdapterUsesCoreWorkers(t *testing.T) {
	cfg := &config.Configuration{Transaction: config.TransactionConfig{BatchSize: 8, MaxQueueSize: 8}}
	ledger := &LedgerForge{config: cfg}
	const count = 64
	var processed atomic.Int64
	var active atomic.Int64
	var peak atomic.Int64
	err := ledger.runReconciliationBatches(context.Background(), "upload", big.NewInt(0), 8,
		func(_ context.Context, id string, limit int, offset int64) ([]*model.Transaction, error) {
			require.Equal(t, "upload", id)
			require.Equal(t, 8, limit)
			if offset >= count {
				return nil, nil
			}
			transactions := make([]*model.Transaction, limit)
			for i := range transactions {
				transactions[i] = &model.Transaction{}
			}
			return transactions, nil
		}, func(context.Context, *model.Transaction) error {
			current := active.Add(1)
			for old := peak.Load(); current > old; old = peak.Load() {
				if peak.CompareAndSwap(old, current) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			processed.Add(1)
			active.Add(-1)
			return nil
		})
	require.NoError(t, err)
	require.Equal(t, int64(count), processed.Load())
	require.Greater(t, peak.Load(), int64(1))
	require.Zero(t, active.Load(), "adapter must join all workers")
}

func TestReconciliationBatchAdapterJoinsWorkersOnFetchError(t *testing.T) {
	ledger := &LedgerForge{config: &config.Configuration{Transaction: config.TransactionConfig{BatchSize: 1, MaxQueueSize: 1}}}
	entered := make(chan struct{})
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	fetchError := errors.New("fetch failed")
	finished := make(chan error, 1)
	go func() {
		finished <- ledger.runReconciliationBatches(context.Background(), "upload", big.NewInt(0), 1,
			func(_ context.Context, _ string, _ int, offset int64) ([]*model.Transaction, error) {
				if offset == 0 {
					return []*model.Transaction{{}}, nil
				}
				<-entered
				return nil, fetchError
			}, func(context.Context, *model.Transaction) error {
				close(entered)
				<-release
				return nil
			})
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	select {
	case <-finished:
		t.Fatal("adapter returned while worker was running")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-finished:
		require.ErrorIs(t, err, fetchError)
	case <-time.After(time.Second):
		t.Fatal("adapter did not finish")
	}
}
