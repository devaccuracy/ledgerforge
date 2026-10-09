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
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/devaccuracy/ledgerforge"
	"github.com/devaccuracy/ledgerforge/database"
	"github.com/devaccuracy/ledgerforge/model"
	"github.com/stretchr/testify/require"
)

type precisionCountingDatasource struct {
	database.IDataSource
	calls        atomic.Int64
	failure      error
	children     []*model.Transaction
	childFailure error
}

func (d *precisionCountingDatasource) GetTransaction(ctx context.Context, id string) (*model.Transaction, error) {
	d.calls.Add(1)
	if d.failure != nil {
		return nil, d.failure
	}
	return d.IDataSource.GetTransaction(ctx, id)
}

func (d *precisionCountingDatasource) GetTransactionsByParent(ctx context.Context, id string, limit int, offset int64) ([]*model.Transaction, error) {
	if d.childFailure != nil {
		return nil, d.childFailure
	}
	if d.children != nil {
		if offset != 0 {
			return nil, nil
		}
		return d.children, nil
	}
	return d.IDataSource.GetTransactionsByParent(ctx, id, limit, offset)
}

func TestInflightAmountSentinelsAndValidation(t *testing.T) {
	// These inputs do not require a precision lookup, so no service is needed.
	a := Api{}
	amount, err := a.resolveInflightAmount(context.Background(), "txn", 0, "0", nil)
	require.NoError(t, err)
	require.Zero(t, amount.Sign())
	precise := big.NewInt(101)
	amount, err = a.resolveInflightAmount(context.Background(), "txn", 5, "5", precise)
	require.NoError(t, err)
	require.Equal(t, precise, amount)
	require.NotSame(t, precise, amount)
	for _, value := range []float64{-1, math.NaN(), math.Inf(1)} {
		_, err = a.resolveInflightAmount(context.Background(), "txn", value, "", nil)
		require.Error(t, err)
	}
	for _, value := range []*big.Int{big.NewInt(0), big.NewInt(-1)} {
		_, err = a.resolveInflightAmount(context.Background(), "txn", 0, "0", value)
		require.Error(t, err)
	}
}

func TestInflightPrecisionLookupConcurrentPostgres(t *testing.T) {
	if testing.Short() {
		t.Skip("requires isolated PostgreSQL and Redis")
	}
	_, service, _ := setupRouterWithConfig(t, nil)
	t.Cleanup(func() { require.NoError(t, service.Close()) })
	source := createTestBalanceWithLedger(t, service, "USD", nil)
	destination := createTestBalanceWithLedger(t, service, "USD", nil)
	ids := make([]string, 2)
	for i, precision := range []float64{100, 1000} {
		tx, err := service.QueueTransaction(context.Background(), &model.Transaction{
			Amount: 10, Precision: precision, Currency: "USD", Reference: model.GenerateUUIDWithSuffix("precision"),
			Description: "concurrent precision lookup", Source: source.BalanceID, Destination: destination.BalanceID,
			AllowOverdraft: true, Inflight: true, SkipQueue: true,
		})
		require.NoError(t, err)
		ids[i] = tx.TransactionID
	}
	counted := &precisionCountingDatasource{IDataSource: service.GetDataSource()}
	injected, err := ledgerforge.NewLedgerForge(counted)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, injected.Close()) })
	a := Api{ledgerforge: injected}
	const workers = 16
	const reads = 8
	start := make(chan struct{})
	failures := make(chan error, workers)
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			<-start
			for read := 0; read < reads; read++ {
				index := (worker + read) % len(ids)
				got, err := a.resolveInflightAmount(context.Background(), ids[index], 1.005, "1.005", nil)
				want := []string{"101", "1005"}[index]
				if err != nil {
					failures <- err
					return
				}
				if got.String() != want {
					failures <- fmt.Errorf("got %s, want %s", got, want)
					return
				}
			}
		}(worker)
	}
	close(start)
	wg.Wait()
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	require.Equal(t, int64(workers*reads), counted.calls.Load(), "every precision read must use the injected datasource")
	// Split parents without their own row still resolve through child rows.
	secondDestination := createTestBalanceWithLedger(t, service, "USD", nil)
	parent, err := service.QueueTransaction(context.Background(), &model.Transaction{
		Amount: 10, Precision: 100, Currency: "USD", Reference: model.GenerateUUIDWithSuffix("precision_parent"),
		Description: "split parent precision", Source: source.BalanceID, AllowOverdraft: true, Inflight: true, SkipQueue: true,
		Destinations: []model.Distribution{{Identifier: destination.BalanceID, Distribution: "50%"}, {Identifier: secondDestination.BalanceID, Distribution: "left"}},
	})
	require.NoError(t, err)
	got, err := a.resolveInflightAmount(context.Background(), parent.TransactionID, 1.005, "1.005", nil)
	require.NoError(t, err)
	require.Equal(t, "101", got.String())
	// A nonexistent ID must preserve the authoritative lookup failure.
	_, err = a.resolveInflightAmount(context.Background(), model.GenerateUUIDWithSuffix("missing"), 1.005, "1.005", nil)
	require.Error(t, err)
	// Mixed-scale parents and failed child lookups must fail closed.
	counted.failure = sql.ErrNoRows
	counted.children = []*model.Transaction{{Precision: 100}, {Precision: 1000}}
	_, err = a.resolveInflightAmount(context.Background(), ids[0], 1.005, "1.005", nil)
	require.ErrorContains(t, err, "mixed precisions")
	counted.children = nil
	counted.childFailure = errors.New("child precision source unavailable")
	_, err = a.resolveInflightAmount(context.Background(), ids[0], 1.005, "1.005", nil)
	require.ErrorIs(t, err, counted.childFailure)
	counted.childFailure = nil
	// A warm transaction ID must still surface a later authoritative error.
	counted.failure = errors.New("precision source unavailable")
	_, err = a.resolveInflightAmount(context.Background(), ids[0], 1.005, "1.005", nil)
	require.ErrorIs(t, err, counted.failure)
}
