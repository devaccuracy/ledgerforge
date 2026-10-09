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

package reconciliation

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/devaccuracy/ledgerforge/model"
	"github.com/sirupsen/logrus"
	"github.com/wacul/ptr"
)

// Status constants representing the various states a process can be in.
const (
	StatusStarted    = "started"     // Indicates the process has started.
	StatusInProgress = "in_progress" // Indicates the process is ongoing.
	StatusCompleted  = "completed"   // Indicates the process is finished successfully.
	StatusFailed     = "failed"      // Indicates the process has failed.
)

// transactionProcessor represents the processor for handling reconciliation-related transactions.
// Fields:
// - reconciliation: The reconciliation object that holds transaction data to be processed.
// - progress: Tracks the progress of the reconciliation process.
// - reconciler: A function that handles the reconciliation logic for a batch of transactions.
// - matches: Counter for transactions that have been successfully matched.
// - unmatched: Counter for transactions that couldn't be matched.
// - datasource: The interface for database operations, enabling interaction with the data source.
// - progressSaveCount: The number of transactions processed before saving progress.
type transactionProcessor struct {
	reconciliation    model.Reconciliation
	progress          model.ReconciliationProgress
	reconciler        reconciler
	matches           int
	unmatched         int
	datasource        Repository
	progressSaveCount int
	updateMetadata    func(context.Context, string, map[string]interface{}) error
	mu                sync.Mutex // guards matches, unmatched, and progress (process runs concurrently)
}

// reconciler defines the function type for reconciling a batch of transactions.
// It accepts a context, and a slice of transactions, and returns the matched transactions and unmatched ones.
type reconciler func(ctx context.Context, txns []*model.Transaction) ([]model.Match, []string)

// loadReconciliationProgress retrieves the progress of a reconciliation process.
// Parameters:
// - ctx: The context controlling the request.
// - reconciliationID: The ID of the reconciliation to load progress for.
// Returns:
// - model.ReconciliationProgress: The current progress of the reconciliation.
// - error: If the progress cannot be retrieved.
func (s *Service) loadReconciliationProgress(ctx context.Context, reconciliationID string) (model.ReconciliationProgress, error) {
	return s.datasource.LoadReconciliationProgress(ctx, reconciliationID)
}

// processReconciliation manages the full reconciliation process by executing each step: updating the status,
// fetching rules, processing transactions, and finalizing the reconciliation.
// Parameters:
// - ctx: The context controlling the process.
// - reconciliation: The reconciliation object representing the current reconciliation.
// - strategy: The reconciliation strategy (e.g., one-to-one, one-to-many).
// - groupCriteria: Criteria for grouping transactions (optional).
// - matchingRuleIDs: A list of matching rule IDs to apply during the process.
// Returns:
// - error: If any step in the reconciliation process fails.
func (s *Service) processReconciliation(ctx context.Context, reconciliation model.Reconciliation, strategy string, groupCriteria string, matchingRuleIDs []string) error {
	// Update the reconciliation status to "in progress".
	if err := s.updateReconciliationStatus(ctx, reconciliation.ReconciliationID, StatusInProgress); err != nil {
		return fmt.Errorf("failed to update reconciliation status: %w", err)
	}

	// Retrieve the matching rules that apply to the reconciliation.
	matchingRules, err := s.getMatchingRules(ctx, matchingRuleIDs)
	if err != nil {
		return fmt.Errorf("failed to get matching rules: %w", err)
	}

	// Initialize the reconciliation progress.
	progress, err := s.initializeReconciliationProgress(ctx, reconciliation.ReconciliationID)
	if err != nil {
		return fmt.Errorf("failed to initialize reconciliation progress: %w", err)
	}

	// Create the reconciler function based on the strategy and rules.
	reconciler := s.createReconciler(strategy, reconciliation.UploadID, groupCriteria, matchingRules)

	// Create a transaction processor to handle the reconciliation logic.
	processor := s.createTransactionProcessor(reconciliation, progress, reconciler)

	// Process the transactions for reconciliation based on the chosen strategy.
	err = s.processTransactions(ctx, reconciliation.UploadID, processor, strategy)
	if err != nil {
		return fmt.Errorf("failed to process transactions: %w", err)
	}

	// After processing, retrieve the results (matched and unmatched counts).
	matched, unmatched := processor.getResults()

	// Finalize the reconciliation by updating the status and recording the results.
	return s.finalizeReconciliation(ctx, reconciliation, matched, unmatched)
}

// updateReconciliationStatus updates the status of a reconciliation process in the database.
// Parameters:
// - ctx: The context controlling the request.
// - reconciliationID: The ID of the reconciliation.
// - status: The new status to set.
// Returns:
// - error: If the status update fails.
func (s *Service) updateReconciliationStatus(ctx context.Context, reconciliationID, status string) error {
	return s.datasource.UpdateReconciliationStatus(ctx, reconciliationID, status, 0, 0)
}

// initializeReconciliationProgress initializes or retrieves the progress of a reconciliation.
// If no progress exists, it creates a new progress entry.
// Parameters:
// - ctx: The context controlling the request.
// - reconciliationID: The ID of the reconciliation.
// Returns:
// - model.ReconciliationProgress: The current or newly initialized progress.
// - error: If the initialization or retrieval fails.
func (s *Service) initializeReconciliationProgress(ctx context.Context, reconciliationID string) (model.ReconciliationProgress, error) {
	progress, err := s.loadReconciliationProgress(ctx, reconciliationID)
	if err != nil {
		logrus.Errorf("Error loading reconciliation progress: %v", err)
		return model.ReconciliationProgress{}, nil
	}
	return progress, nil
}

// createReconciler creates a reconciler function based on the specified strategy, group criteria, and matching rules.
// Parameters:
// - strategy: The reconciliation strategy (e.g., one-to-one, one-to-many).
// - groupCriteria: Criteria for grouping transactions (optional).
// - matchingRules: A list of matching rules to apply during reconciliation.
// Returns:
// - reconciler: A function that performs reconciliation according to the specified strategy.
func (s *Service) createReconciler(strategy string, uploadID string, groupCriteria string, matchingRules []model.MatchingRule) reconciler {
	return func(ctx context.Context, txns []*model.Transaction) ([]model.Match, []string) {
		switch strategy {
		case "one_to_one":
			// Perform one-to-one reconciliation.
			return s.oneToOneReconciliation(ctx, txns, matchingRules)
		case "one_to_many":
			// Perform one-to-many reconciliation.
			return s.oneToManyReconciliation(ctx, txns, groupCriteria, matchingRules, false)
		case "many_to_one":
			// Perform many-to-one reconciliation.
			return s.manyToOneReconciliation(ctx, txns, uploadID, groupCriteria, matchingRules, true)
		default:
			logrus.WithField("strategy", strategy).Warn("unsupported reconciliation strategy")
			return nil, nil
		}
	}
}

// createTransactionProcessor creates a new transaction processor for the reconciliation.
// Parameters:
// - reconciliation: The reconciliation object representing the current process.
// - progress: The current progress of the reconciliation.
// - reconciler: The reconciler function to apply.
// Returns:
// - *transactionProcessor: The created transaction processor.
func (s *Service) createTransactionProcessor(reconciliation model.Reconciliation, progress model.ReconciliationProgress, reconciler func(ctx context.Context, txns []*model.Transaction) ([]model.Match, []string)) *transactionProcessor {
	conf := s.config()
	return &transactionProcessor{
		reconciliation:    reconciliation,
		progress:          progress,
		reconciler:        reconciler,
		datasource:        s.datasource,
		progressSaveCount: conf.ProgressInterval,
		updateMetadata:    s.updateMetadata,
	}
}

// process handles individual transaction processing, applying the reconciliation logic and recording results.
// It also updates internal transaction metadata asynchronously when matches are found.
// Parameters:
// - ctx: The context controlling the request.
// - txn: The transaction to process.
// Returns:
// - error: If processing or recording results fails.
func (tp *transactionProcessor) process(ctx context.Context, txn *model.Transaction) error {
	// Reconcile the batch of transactions and get matches and unmatched transactions.
	batchMatches, batchUnmatched := tp.reconciler(ctx, []*model.Transaction{txn})

	// Increment the counters for matched and unmatched transactions.
	tp.mu.Lock()
	tp.matches += len(batchMatches)
	tp.unmatched += len(batchUnmatched)
	tp.mu.Unlock()

	// If the reconciliation is not a dry run, record the matches and unmatched transactions.
	if !tp.reconciliation.IsDryRun {
		if len(batchMatches) > 0 {
			// Record the matched transactions.
			if err := tp.datasource.RecordMatches(ctx, tp.reconciliation.ReconciliationID, batchMatches); err != nil {
				return err
			}

			// Asynchronously update the metadata for each matched internal transaction
			go tp.updateMatchedTransactionsMetadata(context.Background(), batchMatches)
		}

		if len(batchUnmatched) > 0 {
			// Record the unmatched transactions.
			if err := tp.datasource.RecordUnmatched(ctx, tp.reconciliation.ReconciliationID, batchUnmatched); err != nil {
				return err
			}
		}
	}

	// Update the progress with the last processed transaction ID and increment the processed count.
	tp.mu.Lock()
	tp.progress.LastProcessedExternalTxnID = txn.TransactionID
	tp.progress.ProcessedCount++
	shouldSave := tp.progressSaveCount > 0 && tp.progress.ProcessedCount%tp.progressSaveCount == 0
	progressSnapshot := tp.progress
	defer tp.mu.Unlock()

	// Periodically save the reconciliation progress.
	if shouldSave {
		if err := tp.datasource.SaveReconciliationProgress(ctx, tp.reconciliation.ReconciliationID, progressSnapshot); err != nil {
			logrus.Errorf("Error saving reconciliation progress: %v", err)
		}
	}

	return nil
}

// updateMatchedTransactionsMetadata updates the metadata for internal transactions that were matched.
// This function adds reconciliation information to the internal transaction's metadata.
// Parameters:
// - ctx: The context controlling the operation.
// - matches: The list of matches to process.
func (tp *transactionProcessor) updateMatchedTransactionsMetadata(ctx context.Context, matches []model.Match) {
	for _, match := range matches {
		// Prepare metadata with reconciliation information
		metadata := map[string]interface{}{
			"reconciled":            true,
			"reconciliation_id":     tp.reconciliation.ReconciliationID,
			"reconciled_at":         time.Now().Format(time.RFC3339),
			"external_txn_id":       match.ExternalTransactionID,
			"reconciliation_amount": match.Amount,
		}

		// Update the internal transaction's metadata
		err := tp.updateMetadata(ctx, match.InternalTransactionID, metadata)
		if err != nil {
			logrus.Errorf("Error updating metadata for transaction %s: %v", match.InternalTransactionID, err)
		}
	}
}

// getResults returns the total counts of matched and unmatched transactions processed during reconciliation.
// Returns:
// - int: The count of matched transactions.
// - int: The count of unmatched transactions.
func (tp *transactionProcessor) getResults() (int, int) {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	return tp.matches, tp.unmatched
}

// processTransactions processes the transactions in batches, applying the reconciliation logic for each transaction.
// Parameters:
// - ctx: The context controlling the request.
// - uploadID: The ID of the uploaded transaction file to reconcile.
// - processor: The transaction processor handling the reconciliation logic.
// - strategy: The reconciliation strategy to apply.
// Returns:
// - error: If any error occurs during processing.
func (s *Service) processTransactions(ctx context.Context, uploadID string, processor *transactionProcessor, strategy string) error {
	conf := s.config()
	var processedCount atomic.Int64
	var fetch FetchTransactions
	if strategy == "many_to_one" {
		fetch = s.getInternalTransactionsPaginated
	} else {
		fetch = s.getExternalTransactionsPaginated
	}
	err := s.runBatches(ctx, uploadID, big.NewInt(0), conf.MaxWorkers, fetch,
		func(ctx context.Context, txn *model.Transaction) error {
			if err := processor.process(ctx, txn); err != nil {
				return err
			}
			count := processedCount.Add(1)
			if count%10 == 0 {
				logrus.Infof("Processed %d transactions", count)
			}
			return nil
		})
	logrus.Infof("Total transactions processed: %d", processedCount.Load())
	return err
}

// finalizeReconciliation finalizes the reconciliation process by updating its status and recording the final match/unmatch counts.
// Parameters:
// - ctx: The context controlling the request.
// - reconciliation: The reconciliation object representing the current process.
// - matchCount: The number of matched transactions.
// - unmatchedCount: The number of unmatched transactions.
// Returns:
// - error: If any error occurs during finalization.
func (s *Service) finalizeReconciliation(ctx context.Context, reconciliation model.Reconciliation, matchCount, unmatchedCount int) error {
	// Update the reconciliation status to "completed".
	reconciliation.Status = StatusCompleted
	reconciliation.UnmatchedTransactions = unmatchedCount
	reconciliation.MatchedTransactions = matchCount
	reconciliation.CompletedAt = ptr.Time(time.Now())

	logrus.Infof("Finalizing reconciliation. Matches: %d, Unmatched: %d", matchCount, unmatchedCount)

	if !reconciliation.IsDryRun {
		s.postReconciliationActions(ctx, reconciliation)
	} else {
		logrus.Infof("Dry run completed. Matches: %d, Unmatched: %d", matchCount, unmatchedCount)
	}

	// Update the final reconciliation status and counts in the data source.
	err := s.datasource.UpdateReconciliationStatus(ctx, reconciliation.ReconciliationID, StatusCompleted, matchCount, unmatchedCount)
	if err != nil {
		logrus.Errorf("Error updating reconciliation status: %v", err)
		return err
	}

	logrus.Infof("Reconciliation %s completed. Total matches: %d, Total unmatched: %d", reconciliation.ReconciliationID, matchCount, unmatchedCount)

	return nil
}

// getExternalTransactionsPaginated retrieves paginated external transactions from the data source.
// Parameters:
// - ctx: The context controlling the request.
// - uploadID: The ID of the upload to retrieve transactions for.
// - limit: The maximum number of transactions to retrieve.
// - offset: The offset for pagination.
// Returns:
// - []*model.Transaction: A list of external transactions converted to internal transactions.
// - error: If any error occurs during retrieval.
func (s *Service) getExternalTransactionsPaginated(ctx context.Context, uploadID string, limit int, offset int64) ([]*model.Transaction, error) {
	logrus.Debugf("Fetching external transactions: uploadID=%s, limit=%d, offset=%d", uploadID, limit, offset)
	externalTransaction, err := s.datasource.GetExternalTransactionsPaginated(ctx, uploadID, limit, int64(offset))
	if err != nil {
		logrus.Errorf("Error fetching external transactions: %v", err)
		return nil, err
	}
	logrus.Debugf("Fetched %d external transactions", len(externalTransaction))
	transactions := make([]*model.Transaction, len(externalTransaction))

	for i, txn := range externalTransaction {
		transactions[i] = txn.ToInternalTransaction()
	}
	return transactions, nil
}

// getInternalTransactionsPaginated retrieves paginated internal transactions from the data source.
// Parameters:
// - ctx: The context controlling the request.
// - id: The ID of the internal transactions to retrieve.
// - limit: The maximum number of transactions to retrieve.
// - offset: The offset for pagination.
// Returns:
// - []*model.Transaction: A list of internal transactions.
// - error: If any error occurs during retrieval.
func (s *Service) getInternalTransactionsPaginated(ctx context.Context, id string, limit int, offset int64) ([]*model.Transaction, error) {
	return s.datasource.GetTransactionsPaginated(ctx, "", limit, offset)
}
