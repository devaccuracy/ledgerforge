/*
Copyright 2024 Blnk Finance Authors.
Copyright 2025 LedgerForge Contributors.

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
	"io"
	"math/big"
	"sync"

	"github.com/devaccuracy/ledgerforge/internal/reconciliation"
	"github.com/devaccuracy/ledgerforge/model"
)

const (
	StatusStarted    = reconciliation.StatusStarted
	StatusInProgress = reconciliation.StatusInProgress
	StatusCompleted  = reconciliation.StatusCompleted
	StatusFailed     = reconciliation.StatusFailed
)

func (b *LedgerForge) reconciliationService() *reconciliation.Service {
	b.reconciliationOnce.Do(func() {
		b.reconciliation = reconciliation.NewService(b.datasource, func() reconciliation.Config {
			cfg := b.Config()
			return reconciliation.Config{MaxWorkers: cfg.Transaction.MaxWorkers, BatchSize: cfg.Transaction.BatchSize,
				ProgressInterval: cfg.Reconciliation.ProgressInterval}
		}, b.runReconciliationBatches, func(ctx context.Context, id string, metadata map[string]interface{}) error {
			return b.updateEntityMetadata(ctx, "transactions", id, metadata)
		}, func(rec model.Reconciliation) {
			if err := b.queue.queueIndexData(rec.ReconciliationID, "reconciliations", rec); err != nil {
				b.notifyError(err)
			}
		})
	})
	return b.reconciliation
}

func (b *LedgerForge) runReconciliationBatches(ctx context.Context, id string, amount *big.Int, workers int,
	fetch reconciliation.FetchTransactions, process reconciliation.ProcessTransaction,
) error {
	// Wait for injected workers even if the legacy runner returns a fetch error early.
	var finished sync.WaitGroup
	finished.Add(workers)
	_, err := b.ProcessTransactionInBatches(ctx, id, amount, workers, false, getTxns(fetch),
		func(ctx context.Context, jobs <-chan *model.Transaction, results chan<- BatchJobResult, wg *sync.WaitGroup, _ *big.Int) {
			defer wg.Done()
			defer finished.Done()
			for txn := range jobs {
				results <- BatchJobResult{Error: process(ctx, txn)}
			}
		})
	finished.Wait()
	return err
}

func (b *LedgerForge) UploadExternalData(ctx context.Context, source string, reader io.Reader, filename string) (string, int, error) {
	return b.reconciliationService().UploadExternalData(ctx, source, reader, filename)
}
func (b *LedgerForge) StartReconciliation(ctx context.Context, uploadID, strategy, groupCriteria string, matchingRuleIDs []string, isDryRun bool) (string, error) {
	return b.reconciliationService().StartReconciliation(ctx, uploadID, strategy, groupCriteria, matchingRuleIDs, isDryRun)
}
func (b *LedgerForge) StartInstantReconciliation(ctx context.Context, transactions []model.ExternalTransaction, strategy, groupCriteria string, matchingRuleIDs []string, isDryRun bool) (string, error) {
	return b.reconciliationService().StartInstantReconciliation(ctx, transactions, strategy, groupCriteria, matchingRuleIDs, isDryRun)
}
func (b *LedgerForge) GetReconciliation(ctx context.Context, id string) (*model.Reconciliation, error) {
	return b.reconciliationService().GetReconciliation(ctx, id)
}
func (b *LedgerForge) CreateMatchingRule(ctx context.Context, rule model.MatchingRule) (*model.MatchingRule, error) {
	return b.reconciliationService().CreateMatchingRule(ctx, rule)
}
func (b *LedgerForge) GetMatchingRule(ctx context.Context, id string) (*model.MatchingRule, error) {
	return b.reconciliationService().GetMatchingRule(ctx, id)
}
func (b *LedgerForge) UpdateMatchingRule(ctx context.Context, rule model.MatchingRule) (*model.MatchingRule, error) {
	return b.reconciliationService().UpdateMatchingRule(ctx, rule)
}
func (b *LedgerForge) DeleteMatchingRule(ctx context.Context, id string) error {
	return b.reconciliationService().DeleteMatchingRule(ctx, id)
}
func (b *LedgerForge) ListMatchingRules(ctx context.Context) ([]*model.MatchingRule, error) {
	return b.reconciliationService().ListMatchingRules(ctx)
}
